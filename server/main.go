package main

import (
	"context"

	"github.com/yuelinwen/cabinet-kv-store/server/controllers"
	"github.com/yuelinwen/cabinet-kv-store/server/database"

	"github.com/gin-gonic/gin"
)

func main() {
	// Connect to MongoDB Atlas
	database.ConnectMongoDB()
	defer func() {
		if err := database.MongoClient.Disconnect(context.TODO()); err != nil {
			panic(err)
		}
	}()

	// Load dummy data from CSV into our local Map store
	controllers.InitStore()

	// Initialize Gin router
	router := gin.Default()

	// Register routes
	// return a list of all customers
	router.GET("/customers", controllers.GetCustomers)
	// get customer by ID
	router.GET("/customers/:id", controllers.GetCustomerByID)
	// add a new customer
	router.POST("/customers", controllers.PostCustomer)
	// update an existing customer by ID
	router.PUT("/customers/:id", controllers.PutCustomerByID)
	// delete a customer by ID
	router.DELETE("/customers/:id", controllers.DeleteCustomerByID)

	// Start the server
	router.Run("localhost:8080")
}
