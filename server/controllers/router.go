package controllers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/yuelinwen/cabinet-kv-store/server/database"
	"github.com/yuelinwen/cabinet-kv-store/server/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// GET ALL: GetCustomers returns a list of all customers directly from MongoDB.
func GetCustomers(c *gin.Context) {
	var customers []models.Customer

	// Pass an empty bson.M{} to find all documents
	cursor, err := database.CustomerCollection.Find(context.TODO(), bson.M{})
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch customers"})
		return
	}
	defer cursor.Close(context.TODO())

	// Decode all documents into the customers slice
	if err = cursor.All(context.TODO(), &customers); err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode customers"})
		return
	}

	if customers == nil {
		customers = []models.Customer{} // Return empty array instead of null if no data
	}

	c.IndentedJSON(http.StatusOK, customers)
}

// GET BY ID: GetCustomerByID retrieves a specific customer by ID from MongoDB.
func GetCustomerByID(c *gin.Context) {
	id := c.Param("id")
	var customer models.Customer

	// Find the document where "_id" matches the provided ID
	err := database.CustomerCollection.FindOne(context.TODO(), bson.M{"_id": id}).Decode(&customer)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			c.IndentedJSON(http.StatusNotFound, gin.H{"message": "customer not found"})
			return
		}
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	c.IndentedJSON(http.StatusOK, customer)
}

// CREATE NEW CUSTOMER: PostCustomer adds a new customer into MongoDB.
func PostCustomer(c *gin.Context) {
	var newCustomer models.Customer

	if err := c.BindJSON(&newCustomer); err != nil {
		fmt.Println("Error binding JSON:", err)
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": "Missing required fields or invalid data format"})
		return
	}

	// Initialize account balance and registration date, and generate a unique ID for the new customer
	newCustomer.ID = uuid.New().String()
	newCustomer.AccountBalance = 0.0
	newCustomer.RegistrationDate = time.Now().Format(time.DateOnly)

	_, err := database.CustomerCollection.InsertOne(context.TODO(), newCustomer)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			c.IndentedJSON(http.StatusConflict, gin.H{"error": "Customer ID already exists"})
			return
		}
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert customer"})
		return
	}

	c.IndentedJSON(http.StatusCreated, newCustomer)
}

// UPDATE CUSTOMER: PutCustomerByID completely replaces an existing customer's data in MongoDB.
func PutCustomerByID(c *gin.Context) {
	id := c.Param("id")
	var updatedCustomer models.Customer

	if err := c.BindJSON(&updatedCustomer); err != nil {
		fmt.Println("Error binding JSON:", err)
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	updatedCustomer.ID = id

	// Replace the document in MongoDB
	result, err := database.CustomerCollection.ReplaceOne(context.TODO(), bson.M{"_id": id}, updatedCustomer)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Failed to update customer"})
		return
	}

	if result.MatchedCount == 0 {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "customer not found"})
		return
	}

	c.IndentedJSON(http.StatusOK, updatedCustomer)
}

// DELETE: DeleteCustomerByID removes a customer from MongoDB.
func DeleteCustomerByID(c *gin.Context) {
	id := c.Param("id")

	// Delete from MongoDB
	result, err := database.CustomerCollection.DeleteOne(context.TODO(), bson.M{"_id": id})
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete customer"})
		return
	}

	if result.DeletedCount == 0 {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "customer not found"})
		return
	}

	c.IndentedJSON(http.StatusOK, gin.H{"message": "customer deleted successfully"})
}
