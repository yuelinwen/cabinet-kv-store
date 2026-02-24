package database

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/yuelinwen/cabinet-kv-store/server/models"

	"go.mongodb.org/mongo-driver/v2/bson"
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
	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	opts := options.Client().ApplyURI("mongodb+srv://tyleryuelinwen_db_user:S7nRcnG5MtnVCPlz@cluster0.5f59h5a.mongodb.net/?appName=Cluster0").SetServerAPIOptions(serverAPI)

	client, err := mongo.Connect(opts)
	if err != nil {
		panic(err)
	}

	if err := client.Ping(context.TODO(), readpref.Primary()); err != nil {
		panic(err)
	}
	fmt.Println("✅ Pinged your deployment. You successfully connected to MongoDB!")

	MongoClient = client
	CustomerCollection = client.Database("bank_db").Collection("customers")
}

// SeedDatabaseFromCSV checks if MongoDB is empty. If it is, it loads data from CSV.
func SeedDatabaseFromCSV() {
	// 1. Check if the collection already has data
	count, err := CustomerCollection.CountDocuments(context.TODO(), bson.M{})
	if err != nil {
		log.Fatal("Error checking database count:", err)
	}

	if count > 0 {
		fmt.Printf("✅ MongoDB already contains %d records. Skipping CSV import.\n", count)
		return
	}

	fmt.Println("⏳ MongoDB is empty. Importing data from CSV...")

	// 2. Open and read CSV
	file, err := os.Open("./assets/bank_customers.csv")
	if err != nil {
		log.Println("⚠️ Warning: Could not open CSV file. Database remains empty.")
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal("Error reading CSV file:", err)
	}

	// 3. Prepare data for MongoDB insertion
	var documents []interface{}
	for i, row := range records {
		if i == 0 {
			continue // skip header
		}

		age, _ := strconv.Atoi(row[2])
		balance, _ := strconv.ParseFloat(row[8], 64)

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
		documents = append(documents, customer)
	}

	// 4. Insert all records into MongoDB at once
	if len(documents) > 0 {
		_, err = CustomerCollection.InsertMany(context.TODO(), documents)
		if err != nil {
			log.Fatal("Error inserting documents into MongoDB:", err)
		}
		fmt.Printf("✅ Successfully seeded MongoDB with %d customers!\n", len(documents))
	}
}
