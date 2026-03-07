package database

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"

	"github.com/yuelinwen/cabinet-kv-store/server/models"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Per-node MongoDB clients and collections, keyed by node ID.
// sync.Map is used because multiple goroutines call ConnectMongoDB concurrently.
var (
	clients     sync.Map // map[int]*mongo.Client
	collections sync.Map // map[int]*mongo.Collection
)

// ConnectMongoDB connects node `nodeID` to its own database bank_db_<nodeID>.
func ConnectMongoDB(nodeID int) {
	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	opts := options.Client().
		ApplyURI("mongodb+srv://tyleryuelinwen_db_user:S7nRcnG5MtnVCPlz@cluster0.5f59h5a.mongodb.net/?appName=Cluster0").
		SetServerAPIOptions(serverAPI)

	client, err := mongo.Connect(opts)
	if err != nil {
		panic(fmt.Sprintf("[Node %d] MongoDB connect error: %v", nodeID, err))
	}

	if err := client.Ping(context.TODO(), readpref.Primary()); err != nil {
		panic(fmt.Sprintf("[Node %d] MongoDB ping error: %v", nodeID, err))
	}

	dbName := fmt.Sprintf("bank_db_%d", nodeID)
	fmt.Printf("[Node %d] Connected to MongoDB database: %s\n", nodeID, dbName)

	clients.Store(nodeID, client)
	collections.Store(nodeID, client.Database(dbName).Collection("customers"))
}

// GetCollection returns the customers collection for the given node.
func GetCollection(nodeID int) *mongo.Collection {
	val, ok := collections.Load(nodeID)
	if !ok {
		panic(fmt.Sprintf("no collection registered for node %d", nodeID))
	}
	return val.(*mongo.Collection)
}

// DisconnectNode cleanly closes the MongoDB connection for the given node.
func DisconnectNode(nodeID int) error {
	val, ok := clients.Load(nodeID)
	if !ok {
		return nil
	}
	return val.(*mongo.Client).Disconnect(context.TODO())
}

// SeedDatabaseFromCSV seeds node `nodeID`'s database from CSV if it is empty.
func SeedDatabaseFromCSV(nodeID int) {
	col := GetCollection(nodeID)

	count, err := col.CountDocuments(context.TODO(), bson.M{})
	if err != nil {
		log.Fatalf("[Node %d] Error checking document count: %v", nodeID, err)
	}

	if count > 0 {
		fmt.Printf("[Node %d] Database already contains %d records. Skipping CSV import.\n", nodeID, count)
		return
	}

	fmt.Printf("[Node %d] Database is empty. Importing from CSV...\n", nodeID)

	file, err := os.Open("./assets/bank_customers10.csv")
	if err != nil {
		log.Printf("[Node %d] Warning: could not open CSV file. Database remains empty.\n", nodeID)
		return
	}
	defer file.Close()

	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		log.Fatalf("[Node %d] Error reading CSV: %v", nodeID, err)
	}

	var documents []interface{}
	for i, row := range records {
		if i == 0 {
			continue // skip header
		}
		age, _ := strconv.Atoi(row[2])
		balance, _ := strconv.ParseFloat(row[8], 64)
		documents = append(documents, models.Customer{
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
		})
	}

	if len(documents) > 0 {
		if _, err = col.InsertMany(context.TODO(), documents); err != nil {
			log.Fatalf("[Node %d] Error seeding database: %v", nodeID, err)
		}
		fmt.Printf("[Node %d] Seeded database with %d customers.\n", nodeID, len(documents))
	}
}
