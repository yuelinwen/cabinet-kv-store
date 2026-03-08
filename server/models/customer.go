package models

// Customer 是银行客户的数据结构，对应 MongoDB 中的一条记录。
//
// json 标签  → 定义 HTTP 请求/响应的 JSON 字段名
// bson 标签  → 定义存入 MongoDB 的字段名
// binding   → Gin 框架会检查这些字段是否在请求中提供，缺失则返回 400
type Customer struct {
	ID               string  `json:"id" bson:"_id"`                                        // 唯一标识（UUID）
	AccountBalance   float64 `json:"account_balance" bson:"account_balance"`               // 账户余额（新建时自动设为 0）
	RegistrationDate string  `json:"registration_date" bson:"registration_date"`           // 注册日期（新建时自动填入）

	Name        string `json:"name" bson:"name" binding:"required"`                  // 姓名（必填）
	Age         int    `json:"age" bson:"age" binding:"required"`                    // 年龄（必填）
	Gender      string `json:"gender" bson:"gender" binding:"required"`              // 性别（必填）
	Address     string `json:"address" bson:"address" binding:"required"`            // 地址（必填）
	Email       string `json:"email" bson:"email" binding:"required"`                // 邮箱（必填）
	PhoneNumber string `json:"phone_number" bson:"phone_number" binding:"required"`  // 电话（必填）
	AccountType string `json:"account_type" bson:"account_type" binding:"required"`  // 账户类型（必填）
}
