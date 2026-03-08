package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yuelinwen/cabinet-kv-store/server/cabinet"
	"github.com/yuelinwen/cabinet-kv-store/server/database"
	"github.com/yuelinwen/cabinet-kv-store/server/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// CmdCh is injected by main.go on the leader node before Gin starts.
// Write handlers push KVCommands here and wait for the consensus result.
// It is nil on follower nodes (the gateway never routes writes to followers).
var CmdCh chan cabinet.KVCommand

// submitWrite sends cmd through Cabinet consensus and blocks until committed.
func submitWrite(c *gin.Context, cmd cabinet.KVCommand) error {
	if CmdCh == nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": "this node is not the leader"})
		return fmt.Errorf("not leader")
	}
	replyCh := make(chan error, 1)
	cmd.ReplyCh = replyCh
	CmdCh <- cmd
	return <-replyCh
}

// getCollection retrieves the MongoDB collection for the node that received this request.
func getCollection(c *gin.Context) *mongo.Collection {
	nodeID, _ := c.Get("nodeID")
	return database.GetCollection(nodeID.(int))
}

// GET ALL
func GetCustomers(c *gin.Context) {
	var customers []models.Customer

	cursor, err := getCollection(c).Find(context.TODO(), bson.M{})
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch customers"})
		return
	}
	defer cursor.Close(context.TODO())

	if err = cursor.All(context.TODO(), &customers); err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode customers"})
		return
	}

	if customers == nil {
		customers = []models.Customer{}
	}
	c.IndentedJSON(http.StatusOK, customers)
}

// GET BY ID
func GetCustomerByID(c *gin.Context) {
	id := c.Param("id")
	var customer models.Customer

	err := getCollection(c).FindOne(context.TODO(), bson.M{"_id": id}).Decode(&customer)
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

// POST — create new customer via Cabinet consensus
func PostCustomer(c *gin.Context) {
	var newCustomer models.Customer
	if err := c.BindJSON(&newCustomer); err != nil {
		fmt.Println("Error binding JSON:", err)
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": "Missing required fields or invalid data format"})
		return
	}

	newCustomer.ID = uuid.New().String()
	newCustomer.AccountBalance = 0.0
	newCustomer.RegistrationDate = time.Now().Format(time.DateOnly)

	valueJSON, err := json.Marshal(newCustomer)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialise customer"})
		return
	}

	if err := submitWrite(c, cabinet.KVCommand{Op: "INSERT", Key: newCustomer.ID, ValueJSON: string(valueJSON)}); err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.IndentedJSON(http.StatusCreated, newCustomer)
}

// PUT — replace existing customer via Cabinet consensus
func PutCustomerByID(c *gin.Context) {
	id := c.Param("id")
	var updatedCustomer models.Customer

	if err := c.BindJSON(&updatedCustomer); err != nil {
		fmt.Println("Error binding JSON:", err)
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	updatedCustomer.ID = id

	valueJSON, err := json.Marshal(updatedCustomer)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialise customer"})
		return
	}

	if err := submitWrite(c, cabinet.KVCommand{Op: "REPLACE", Key: id, ValueJSON: string(valueJSON)}); err != nil {
		if err.Error() == "customer not found" {
			c.IndentedJSON(http.StatusNotFound, gin.H{"message": "customer not found"})
			return
		}
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.IndentedJSON(http.StatusOK, updatedCustomer)
}

// DELETE — remove customer via Cabinet consensus
func DeleteCustomerByID(c *gin.Context) {
	id := c.Param("id")

	if err := submitWrite(c, cabinet.KVCommand{Op: "DELETE", Key: id}); err != nil {
		if err.Error() == "customer not found" {
			c.IndentedJSON(http.StatusNotFound, gin.H{"message": "customer not found"})
			return
		}
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.IndentedJSON(http.StatusOK, gin.H{"message": "customer deleted successfully"})
}
