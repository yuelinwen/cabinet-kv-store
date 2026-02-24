package database

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/yuelinwen/cabinet-kv-store/server/models"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

var (
	MongoClient        *mongo.Client
	CustomerCollection *mongo.Collection
)

// ConnectMongoDB initializes the connection to the MongoDB Atlas cluster.
func ConnectMongoDB() {
	// Use the SetServerAPIOptions() method to set the version of the Stable API on the client
	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	opts := options.Client().ApplyURI("mongodb+srv://tyleryuelinwen_db_user:S7nRcnG5MtnVCPlz@cluster0.5f59h5a.mongodb.net/?appName=Cluster0").SetServerAPIOptions(serverAPI)

	// Create a new client and connect to the server
	client, err := mongo.Connect(opts)
	if err != nil {
		panic(err)
	}

	// Send a ping to confirm a successful connection
	if err := client.Ping(context.TODO(), readpref.Primary()); err != nil {
		panic(err)
	}
	fmt.Println("✅ Pinged your deployment. You successfully connected to MongoDB!")

	MongoClient = client
	CustomerCollection = client.Database("bank_db").Collection("customers")
}

// LoadCSVToMemory reads the dummy data and stores it in the provided map.
func LoadCSVToMemory(store map[string]models.Customer) {
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

	// retrieve data from csv and store it in the map
	for i, row := range records {
		if i == 0 {
			continue // skip the header row
		}

		// convert strings to correct types
		age, _ := strconv.Atoi(row[2])
		balance, _ := strconv.ParseFloat(row[8], 64)

		// convert the csv row into a Customer struct
		customer := models.Customer{
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

		// save the customer into the store map
		store[customer.ID] = customer
	}

	// print the number of customers loaded from the CSV file
	fmt.Printf("✅ Successfully loaded %d customers from CSV!\n", len(store))
}
