package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"github.com/gin-gonic/gin"
)

// Customer represents the data structure of a bank client.
type Customer struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Age              int     `json:"age"`
	Gender           string  `json:"gender"`
	Address          string  `json:"address"`
	Email            string  `json:"email"`
	PhoneNumber      string  `json:"phone_number"`
	AccountType      string  `json:"account_type"`
	AccountBalance   float64 `json:"account_balance"`
	RegistrationDate string  `json:"registration_date"`
}

// We use a Map as our local Key-Value Store.
// The Key is the customer ID, and the Value is the customer information.
// In Go, the built-in make function is used to create and initialize only three types of built-in data structures: slices, maps, and channels.
var (
	customerStore = make(map[string]Customer)
	storeMutex    sync.RWMutex // Read-Write mutex to ensure concurrency safety
)

// The init function runs automatically when the program starts.
func init() {
	// open csv file
	file, err := os.Open("./assets/bank_customers.csv")
	if err != nil {
		// print error if the file cannot be opened, but don't crash the program. Instead, we can start with an empty database.
		log.Println("⚠️ Warning: Could not open bank_customers.csv. Starting with an empty database.")
		return
	}
	defer file.Close() // close file

	// create csv reader
	reader := csv.NewReader(file)

	// read all records from the csv file
	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal("Error reading CSV file:", err)
	}

	// retrieve data from csv and store it in the customerStore map
	for i, row := range records {
		if i == 0 {
			continue // skip the header row
		}

		// convert the string format of Age to an integer
		age, _ := strconv.Atoi(row[2])
		// convert the string format of AccountBalance to a float
		balance, _ := strconv.ParseFloat(row[8], 64)

		// convert the csv row into a Customer struct
		customer := Customer{
			ID:               row[0],
			Name:             row[1],
			Age:              age,
			Gender:           row[3],
			Address:          row[4],
			Email:            row[5],
			PhoneNumber:      row[6],
			AccountType:      row[7],
			AccountBalance:   balance,
			RegistrationDate: row[9],
		}

		// save the customer into the key-value store
		customerStore[customer.ID] = customer
	}

	// print the number of customers loaded from the CSV file
	fmt.Printf("✅ Successfully loaded %d customers from CSV!\n", len(customerStore))
}

// getCustomers returns a list of all customers.
func getCustomers(c *gin.Context) {
	storeMutex.RLock() // Acquire read lock
	defer storeMutex.RUnlock()

	// Convert the map to a slice to return it as a JSON array
	var customers []Customer
	for _, customer := range customerStore {
		customers = append(customers, customer)
	}
	c.IndentedJSON(http.StatusOK, customers)
}

// postCustomer adds a new customer (corresponds to the PUT/write operation in your project).
func postCustomer(c *gin.Context) {
	var newCustomer Customer

	// Bind the JSON data received from the request body
	if err := c.BindJSON(&newCustomer); err != nil {
		fmt.Println("Error binding JSON:", err)
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	storeMutex.Lock() // Acquire write lock
	defer storeMutex.Unlock()

	// Check if the customer ID already exists
	if _, exists := customerStore[newCustomer.ID]; exists {
		c.IndentedJSON(http.StatusConflict, gin.H{"error": "Customer ID already exists"})
		return
	}

	// Save the new customer into the key-value store
	customerStore[newCustomer.ID] = newCustomer
	c.IndentedJSON(http.StatusCreated, newCustomer)
}

// getCustomerByID retrieves a specific customer by ID (corresponds to the GET operation).
func getCustomerByID(c *gin.Context) {
	id := c.Param("id")

	storeMutex.RLock() // Acquire read lock
	customer, exists := customerStore[id]
	storeMutex.RUnlock()

	if exists {
		c.IndentedJSON(http.StatusOK, customer)
		return
	}

	c.IndentedJSON(http.StatusNotFound, gin.H{"message": "customer not found"})
}

// putCustomerByID updates an existing customer's information (corresponds to the PUT operation).
func putCustomerByID(c *gin.Context) {
	id := c.Param("id")
	var updatedCustomer Customer

	// Bind the JSON data received from the request body
	if err := c.BindJSON(&updatedCustomer); err != nil {
		fmt.Println("Error binding JSON:", err)
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	// Ensure the ID in the payload matches the ID in the URL
	updatedCustomer.ID = id

	storeMutex.Lock() // Acquire write lock
	defer storeMutex.Unlock()

	// Check if the customer exists before updating
	if _, exists := customerStore[id]; !exists {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "customer not found"})
		return
	}

	// Update the customer in the key-value store
	customerStore[id] = updatedCustomer
	c.IndentedJSON(http.StatusOK, updatedCustomer)
}

// deleteCustomerByID removes a customer by ID (corresponds to the DELETE operation).
func deleteCustomerByID(c *gin.Context) {
	id := c.Param("id")

	storeMutex.Lock() // Acquire write lock
	_, exists := customerStore[id]
	if exists {
		delete(customerStore, id) // Delete this key from the map
	}
	storeMutex.Unlock()

	if exists {
		c.IndentedJSON(http.StatusOK, gin.H{"message": "customer deleted successfully"})
	} else {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "customer not found"})
	}
}

func main() {
	// Use the SetServerAPIOptions() method to set the version of the Stable API on the client
	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	opts := options.Client().ApplyURI("mongodb+srv://tyleryuelinwen_db_user:S7nRcnG5MtnVCPlz@cluster0.5f59h5a.mongodb.net/?appName=Cluster0").SetServerAPIOptions(serverAPI)
	// Create a new client and connect to the server
	client, err := mongo.Connect(opts)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err = client.Disconnect(context.TODO()); err != nil {
			panic(err)
		}
	}()
	// Send a ping to confirm a successful connection
	if err := client.Ping(context.TODO(), readpref.Primary()); err != nil {
		panic(err)
	}
	fmt.Println("Pinged your deployment. You successfully connected to MongoDB!")

	router := gin.Default()

	// Register routes
	// return a list of all customers
	router.GET("/customers", getCustomers)
	// get customer by ID
	router.GET("/customers/:id", getCustomerByID)
	// add a new customer
	router.POST("/customers", postCustomer)
	// update an existing customer by ID
	router.PUT("/customers/:id", putCustomerByID)
	// delete a customer by ID
	router.DELETE("/customers/:id", deleteCustomerByID)

	// Start the server
	router.Run("localhost:8080")
}
