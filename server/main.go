package main

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

// Customer represents the data structure of a bank client.
type Customer struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	AccountType string  `json:"account_type"` // e.g., Checking, Savings
	Balance     float64 `json:"balance"`      // Account balance
}

// We use a Map as our local Key-Value Store.
// The Key is the customer ID, and the Value is the customer information.
var (
	customerStore = make(map[string]Customer)
	storeMutex    sync.RWMutex // Read-Write mutex to ensure concurrency safety
)

// The init function runs automatically when the program starts.
// We seed it with two initial test records.
func init() {
	customerStore["1001"] = Customer{ID: "1001", Name: "Alice Smith", AccountType: "Checking", Balance: 1500.50}
	customerStore["1002"] = Customer{ID: "1002", Name: "Bob Jones", AccountType: "Savings", Balance: 12000.00}
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
	router := gin.Default()

	// Register routes
	router.GET("/customers", getCustomers)
	router.GET("/customers/:id", getCustomerByID)
	router.POST("/customers", postCustomer)
	router.DELETE("/customers/:id", deleteCustomerByID) // Added DELETE route

	// Start the server
	router.Run("localhost:8080")
}
