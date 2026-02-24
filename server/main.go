package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

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
	// 打开 CSV 文件
	file, err := os.Open("./assets/bank_customers.csv")
	if err != nil {
		// 如果找不到文件，打印警告但不崩溃，数据库保持为空
		log.Println("⚠️ Warning: Could not open bank_customers.csv. Starting with an empty database.")
		return
	}
	defer file.Close() // 记得在函数结束时关闭文件

	// 创建 CSV 读取器
	reader := csv.NewReader(file)

	// 读取所有行
	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal("Error reading CSV file:", err)
	}

	// 遍历每一行数据
	for i, row := range records {
		if i == 0 {
			continue // 跳过第一行的表头 (Header)
		}

		// 将字符串格式的 Age 转换为 int
		age, _ := strconv.Atoi(row[2])
		// 将字符串格式的 Balance 转换为 float64
		balance, _ := strconv.ParseFloat(row[8], 64)

		// 将这一行的数据组装成一个 Customer 对象
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

		// 存入我们的内存数据库
		customerStore[customer.ID] = customer
	}

	// 启动时在终端打印加载了多少条数据
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
