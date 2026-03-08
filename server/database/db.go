package database

// 【数据库层】
// 每个节点连接自己独立的 MongoDB 数据库（bank_db_0 / bank_db_1 / bank_db_2）
// 用 sync.Map 存多个节点的连接，key = nodeID，并发安全

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

// clients     存各节点的 MongoDB 客户端连接，key = nodeID
// collections 存各节点的 customers 集合对象，key = nodeID
// 使用 sync.Map 是因为多个节点在不同 goroutine 里并发调用 ConnectMongoDB
var (
	clients     sync.Map // map[int]*mongo.Client
	collections sync.Map // map[int]*mongo.Collection
)

// ConnectMongoDB 连接节点 nodeID 专属的 MongoDB 数据库 bank_db_{nodeID}
// 在 startNode 启动时调用一次
func ConnectMongoDB(nodeID int) {
	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	opts := options.Client().
		ApplyURI("mongodb+srv://tyleryuelinwen_db_user:S7nRcnG5MtnVCPlz@cluster0.5f59h5a.mongodb.net/?appName=Cluster0").
		SetServerAPIOptions(serverAPI)

	client, err := mongo.Connect(opts)
	if err != nil {
		panic(fmt.Sprintf("[Node %d] MongoDB connect error: %v", nodeID, err))
	}

	// Ping 验证连接真正可用
	if err := client.Ping(context.TODO(), readpref.Primary()); err != nil {
		panic(fmt.Sprintf("[Node %d] MongoDB ping error: %v", nodeID, err))
	}

	dbName := fmt.Sprintf("bank_db_%d", nodeID)
	fmt.Printf("[Node %d] Connected to MongoDB database: %s\n", nodeID, dbName)

	// 存入 sync.Map，key = nodeID
	clients.Store(nodeID, client)
	collections.Store(nodeID, client.Database(dbName).Collection("customers"))
}

// GetCollection 返回 nodeID 节点的 customers 集合
// controllers 通过 gin context 里的 nodeID 调用此函数，确保每个节点只操作自己的数据库
func GetCollection(nodeID int) *mongo.Collection {
	val, ok := collections.Load(nodeID)
	if !ok {
		panic(fmt.Sprintf("no collection registered for node %d", nodeID))
	}
	return val.(*mongo.Collection)
}

// DisconnectNode 在节点退出时断开 MongoDB 连接（配合 defer 使用）
func DisconnectNode(nodeID int) error {
	val, ok := clients.Load(nodeID)
	if !ok {
		return nil
	}
	return val.(*mongo.Client).Disconnect(context.TODO())
}

// SeedDatabaseFromCSV 如果数据库为空，则从 CSV 文件批量导入初始数据
// 幂等操作：已有数据就直接跳过，不会重复导入
func SeedDatabaseFromCSV(nodeID int) {
	col := GetCollection(nodeID)

	// 检查数据库是否已有数据
	count, err := col.CountDocuments(context.TODO(), bson.M{})
	if err != nil {
		log.Fatalf("[Node %d] Error checking document count: %v", nodeID, err)
	}

	if count > 0 {
		fmt.Printf("[Node %d] Database already contains %d records. Skipping CSV import.\n", nodeID, count)
		return
	}

	fmt.Printf("[Node %d] Database is empty. Importing from CSV...\n", nodeID)

	// 打开种子数据文件
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

	// 逐行解析 CSV，跳过表头（第 0 行）
	var documents []interface{}
	for i, row := range records {
		if i == 0 {
			continue // 跳过列标题行
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

	// 批量写入 MongoDB
	if len(documents) > 0 {
		if _, err = col.InsertMany(context.TODO(), documents); err != nil {
			log.Fatalf("[Node %d] Error seeding database: %v", nodeID, err)
		}
		fmt.Printf("[Node %d] Seeded database with %d customers.\n", nodeID, len(documents))
	}
}
