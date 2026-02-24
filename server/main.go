package main

import (
	"context"

	"github.com/yuelinwen/cabinet-kv-store/server/controllers"
	"github.com/yuelinwen/cabinet-kv-store/server/database"

	"github.com/gin-gonic/gin"
)

func main() {
	// 1. Connect to MongoDB Atlas
	database.ConnectMongoDB()
	defer func() {
		if err := database.MongoClient.Disconnect(context.TODO()); err != nil {
			panic(err)
		}
	}()

	// 2. Automatically seed data to MongoDB if it is empty!
	database.SeedDatabaseFromCSV()

	// 3. Initialize Gin router
	router := gin.Default()

	// Register routes
	router.GET("/customers", controllers.GetCustomers)
	router.GET("/customers/:id", controllers.GetCustomerByID)
	router.POST("/customers", controllers.PostCustomer)
	router.PUT("/customers/:id", controllers.PutCustomerByID)
	router.DELETE("/customers/:id", controllers.DeleteCustomerByID)

	// Start the server
	router.Run("localhost:8080")
}
