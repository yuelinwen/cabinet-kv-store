package controllers

// 【HTTP 请求处理层】
//
// 每个 handler 对应一个 REST 接口。
// 读操作（GET）：直接查本节点的 MongoDB，立即返回。
// 写操作（POST/PUT/DELETE）：把命令封装成 KVCommand，推入 CmdCh，
//   阻塞等待 Cabinet 共识完成（ReplyCh），然后返回 HTTP 响应。

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

// CmdCh 由 main.go 在 leader 节点启动时注入。
// 写 handler 把 KVCommand 塞入这里，RunConsensus 从这里取出执行共识。
// follower 节点上此值为 nil（gateway 保证写操作不会发到 follower）。
var CmdCh chan cabinet.KVCommand

// submitWrite 把一条写命令提交给共识层，阻塞直到共识结果返回
// 如果 CmdCh 为 nil（说明当前节点不是 leader），直接返回 503
func submitWrite(c *gin.Context, cmd cabinet.KVCommand) error {
	if CmdCh == nil {
		c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": "this node is not the leader"})
		return fmt.Errorf("not leader")
	}
	replyCh := make(chan error, 1) // 创建一次性结果通道
	cmd.ReplyCh = replyCh
	CmdCh <- cmd     // 把命令推给 RunConsensus
	return <-replyCh // 阻塞，等待共识结果（成功=nil，失败=error）
}

// getCollection 从 gin context 取出 nodeID，返回该节点的 MongoDB collection
// nodeID 由 main.go 的中间件（router.Use）注入每个请求
func getCollection(c *gin.Context) *mongo.Collection {
	nodeID, _ := c.Get("nodeID")
	return database.GetCollection(nodeID.(int))
}

// ── GET /customers ────────────────────────────────────────────────────────
// 查询所有客户，直接读本节点 MongoDB（无需共识）
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

// ── GET /customers/:id ────────────────────────────────────────────────────
// 查询单个客户，直接读本节点 MongoDB（无需共识）
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

// ── POST /customers ───────────────────────────────────────────────────────
// 新建客户：生成 UUID + 初始余额，走 Cabinet 共识写入所有节点
func PostCustomer(c *gin.Context) {
	var newCustomer models.Customer
	if err := c.BindJSON(&newCustomer); err != nil {
		fmt.Println("Error binding JSON:", err)
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": "Missing required fields or invalid data format"})
		return
	}

	// 自动填充服务端字段
	newCustomer.ID = uuid.New().String()                           // 生成唯一 ID
	newCustomer.AccountBalance = 0.0                               // 新账户余额为 0
	newCustomer.RegistrationDate = time.Now().Format(time.DateOnly) // 今天的日期

	// 序列化为 JSON，作为共识命令的 ValueJSON
	valueJSON, err := json.Marshal(newCustomer)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialise customer"})
		return
	}

	// 提交共识，阻塞等待所有节点写入完成
	if err := submitWrite(c, cabinet.KVCommand{Op: "INSERT", Key: newCustomer.ID, ValueJSON: string(valueJSON)}); err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.IndentedJSON(http.StatusCreated, newCustomer) // 201 Created
}

// ── PUT /customers/:id ────────────────────────────────────────────────────
// 替换客户数据：走 Cabinet 共识更新所有节点
func PutCustomerByID(c *gin.Context) {
	id := c.Param("id")
	var updatedCustomer models.Customer

	if err := c.BindJSON(&updatedCustomer); err != nil {
		fmt.Println("Error binding JSON:", err)
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	updatedCustomer.ID = id // 用 URL 中的 ID 覆盖 body 里可能传来的 ID

	valueJSON, err := json.Marshal(updatedCustomer)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialise customer"})
		return
	}

	// 提交共识
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

// ── DELETE /customers/:id ─────────────────────────────────────────────────
// 删除客户：走 Cabinet 共识从所有节点删除
func DeleteCustomerByID(c *gin.Context) {
	id := c.Param("id")

	// 提交共识（ValueJSON 为空，DELETE 只需要 Key）
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
