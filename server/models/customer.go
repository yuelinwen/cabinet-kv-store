package models

// Customer represents the data structure of a bank client.
type Customer struct {
	ID               string  `json:"id" bson:"_id"`
	Name             string  `json:"name" bson:"name"`
	Age              int     `json:"age" bson:"age"`
	Gender           string  `json:"gender" bson:"gender"`
	Address          string  `json:"address" bson:"address"`
	Email            string  `json:"email" bson:"email"`
	PhoneNumber      string  `json:"phone_number" bson:"phone_number"`
	AccountType      string  `json:"account_type" bson:"account_type"`
	AccountBalance   float64 `json:"account_balance" bson:"account_balance"`
	RegistrationDate string  `json:"registration_date" bson:"registration_date"`
}
