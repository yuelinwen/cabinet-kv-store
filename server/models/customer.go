package models

// Customer represents the data structure of a bank client.
type Customer struct {
	ID               string  `json:"id" bson:"_id"`
	AccountBalance   float64 `json:"account_balance" bson:"account_balance"`
	RegistrationDate string  `json:"registration_date" bson:"registration_date"`

	Name        string `json:"name" bson:"name" binding:"required"`
	Age         int    `json:"age" bson:"age" binding:"required"`
	Gender      string `json:"gender" bson:"gender" binding:"required"`
	Address     string `json:"address" bson:"address" binding:"required"`
	Email       string `json:"email" bson:"email" binding:"required"`
	PhoneNumber string `json:"phone_number" bson:"phone_number" binding:"required"`
	AccountType string `json:"account_type" bson:"account_type" binding:"required"`
}
