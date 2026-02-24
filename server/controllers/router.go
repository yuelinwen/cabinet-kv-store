package controllers

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/yuelinwen/cabinet-kv-store/server/database"
	"github.com/yuelinwen/cabinet-kv-store/server/models"

	"github.com/gin-gonic/gin"
)

// We use a Map as our local Key-Value Store.
// The Key is the customer ID, and the Value is the customer information.
// In Go, the built-in make function is used to create and initialize only three types of built-in data structures: slices, maps, and channels.
var (
	customerStore = make(map[string]models.Customer)
	storeMutex    sync.RWMutex // Read-Write mutex to ensure concurrency safety
)

// InitStore calls the database package to load CSV dummy data into our local map.
func InitStore() {
	database.LoadCSVToMemory(customerStore)
}

// GetCustomers returns a list of all customers.
func GetCustomers(c *gin.Context) {
	storeMutex.RLock() // Acquire read lock
	defer storeMutex.RUnlock()

	// Convert the map to a slice to return it as a JSON array
	var customers []models.Customer
	for _, customer := range customerStore {
		customers = append(customers, customer)
	}
	c.IndentedJSON(http.StatusOK, customers)
}

// PostCustomer adds a new customer (corresponds to the PUT/write operation in your project).
func PostCustomer(c *gin.Context) {
	var newCustomer models.Customer

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

// GetCustomerByID retrieves a specific customer by ID (corresponds to the GET operation).
func GetCustomerByID(c *gin.Context) {
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

// PutCustomerByID updates an existing customer's information (corresponds to the PUT operation).
func PutCustomerByID(c *gin.Context) {
	id := c.Param("id")
	var updatedCustomer models.Customer

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

// DeleteCustomerByID removes a customer by ID (corresponds to the DELETE operation).
func DeleteCustomerByID(c *gin.Context) {
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
