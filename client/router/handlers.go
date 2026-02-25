package router

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const ServerURL = "http://localhost:8080/customers"

type ClientCustomer struct {
	ID               string  `json:"id,omitempty"`
	Name             string  `json:"name"`
	Age              int     `json:"age"`
	Gender           string  `json:"gender"`
	Address          string  `json:"address"`
	Email            string  `json:"email"`
	PhoneNumber      string  `json:"phone_number"`
	AccountType      string  `json:"account_type"`
	AccountBalance   float64 `json:"account_balance"`
	RegistrationDate string  `json:"registration_date,omitempty"`
}

func promptInput(reader *bufio.Reader, prompt string) string {
	fmt.Print(prompt)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func WaitForEnter(reader *bufio.Reader) {
	fmt.Print("\n🔙 Press [ENTER] to return to the Main Menu...")
	reader.ReadString('\n')
}

func ViewAllCustomers() {
	fmt.Println(">> Action: Fetching all customers from the server...")
	resp, err := http.Get(ServerURL)
	if err != nil {
		fmt.Println("❌ Error: Could not fetch data from server.")
		return
	}
	defer resp.Body.Close()

	var customers []ClientCustomer
	json.NewDecoder(resp.Body).Decode(&customers)

	fmt.Printf("✅ Success! Found %d customers.\n\n", len(customers))
	fmt.Printf("%-25s | %-20s | %-10s\n", "ID", "Name", "Balance")
	fmt.Println("------------------------------------------------------------------")

	limit := 10
	if len(customers) < 10 {
		limit = len(customers)
	}

	for i := 0; i < limit; i++ {
		c := customers[i]
		fmt.Printf("%-25s | %-20s | $%.2f\n", c.ID, c.Name, c.AccountBalance)
	}
	if len(customers) > 10 {
		fmt.Printf("... (Showing top 10 results. Total users: %d)\n", len(customers))
	}
}

func SearchCustomer(reader *bufio.Reader) {
	targetID := promptInput(reader, ">> Please enter the Customer ID to search: ")
	if targetID == "" {
		return
	}

	resp, err := http.Get(ServerURL + "/" + targetID)
	if err != nil {
		fmt.Println("❌ Error: Could not connect to the server.")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		fmt.Println("⚠️ Customer not found!")
		return
	}

	var c ClientCustomer
	json.NewDecoder(resp.Body).Decode(&c)
	fmt.Println("\n✅ Customer Found!")
	fmt.Printf("ID: %s | Name: %s | Age: %d | Type: %s | Balance: $%.2f\n", c.ID, c.Name, c.Age, c.AccountType, c.AccountBalance)
}

func CreateCustomer(reader *bufio.Reader) {
	fmt.Println(">> Action: Opening a New Account")
	var c ClientCustomer

	c.Name = promptInput(reader, " 👤 Name: ")
	ageStr := promptInput(reader, " 🎂 Age: ")
	c.Age, _ = strconv.Atoi(ageStr)
	c.Gender = promptInput(reader, " ⚧️  Gender (Male/Female): ")
	c.Address = promptInput(reader, " 🏠 Address: ")
	c.Email = promptInput(reader, " 📧 Email: ")
	c.PhoneNumber = promptInput(reader, " 📞 Phone Number: ")
	c.AccountType = promptInput(reader, " 🏦 Account Type: ")

	jsonData, _ := json.Marshal(c)
	resp, err := http.Post(ServerURL, "application/json", bytes.NewBuffer(jsonData))

	if err != nil {
		fmt.Println("❌ Error: Could not connect to the server.")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var newC ClientCustomer
		json.NewDecoder(resp.Body).Decode(&newC)

		fmt.Println("\n✅ Success! Account created successfully.")
		fmt.Printf("ID: %s | Name: %s | Age: %d | Type: %s | Balance: $%.2f\n", newC.ID, newC.Name, newC.Age, newC.AccountType, newC.AccountBalance)
	} else {
		fmt.Println("❌ Failed to create account.")
	}
}

func UpdateCustomer(reader *bufio.Reader) {
	targetID := promptInput(reader, ">> Please enter the Customer ID to UPDATE: ")
	if targetID == "" {
		return
	}

	fmt.Printf("⏳ Fetching current data for ID '%s'...\n", targetID)
	getResp, err := http.Get(ServerURL + "/" + targetID)
	if err != nil {
		fmt.Println("❌ Error: Could not connect to the server.")
		return
	}
	defer getResp.Body.Close()

	if getResp.StatusCode == http.StatusNotFound {
		fmt.Println("⚠️ Customer not found! Please check the ID.")
		return
	}

	var c ClientCustomer
	if err := json.NewDecoder(getResp.Body).Decode(&c); err != nil {
		fmt.Println("❌ Error: Failed to parse customer data.")
		return
	}

	for {
		fmt.Println("\n---------------- UPDATE MENU ----------------")
		fmt.Printf("Updating Customer: %s (Current Balance: $%.2f)\n", c.Name, c.AccountBalance)
		fmt.Println("[1] Name\n[2] Age\n[3] Address\n[4] Email")
		fmt.Println("[5] Phone Number\n[6] Gender\n[7] Account Type")
		fmt.Println("[8] Action (Deposit / Withdraw)")
		fmt.Println("[0] Finish & Return to Main Menu")
		fmt.Println("---------------------------------------------")

		choice := promptInput(reader, "👉 Which field would you like to update? (0-8): ")

		if choice == "0" {
			fmt.Println("🔙 Exiting update menu...")
			return
		}

		switch choice {
		case "1":
			c.Name = promptInput(reader, " 👤 Enter New Name: ")
		case "2":
			ageStr := promptInput(reader, " 🎂 Enter New Age: ")
			c.Age, _ = strconv.Atoi(ageStr)
		case "3":
			c.Address = promptInput(reader, " 🏠 Enter New Address: ")
		case "4":
			c.Email = promptInput(reader, " 📧 Enter New Email: ")
		case "5":
			c.PhoneNumber = promptInput(reader, " 📞 Enter New Phone Number: ")
		case "6":
			genderChoice := promptInput(reader, " ⚧️  Choose Gender - [1] Male, [2] Female: ")
			if genderChoice == "1" {
				c.Gender = "Male"
			} else if genderChoice == "2" {
				c.Gender = "Female"
			} else {
				continue
			}
		case "7":
			typeChoice := promptInput(reader, " 🏦 Choose Account Type - [1] Checking, [2] Savings: ")
			if typeChoice == "1" {
				c.AccountType = "Checking"
			} else if typeChoice == "2" {
				c.AccountType = "Savings"
			} else {
				continue
			}
		case "8":
			actionChoice := promptInput(reader, " 💰 Action - [1] Deposit, [2] Withdraw: ")
			amountStr := promptInput(reader, " 💲 Enter Amount: ")
			amount, err := strconv.ParseFloat(amountStr, 64)
			if err != nil || amount <= 0 {
				fmt.Println("❌ Invalid amount. Cancelled.")
				continue
			}

			if actionChoice == "1" {
				c.AccountBalance += amount
			} else if actionChoice == "2" {
				if amount > c.AccountBalance {
					fmt.Println("❌ Insufficient funds! Cancelled.")
					continue
				}
				c.AccountBalance -= amount
			} else {
				continue
			}
		default:
			fmt.Println("❌ Invalid choice. Please try again.")
			continue
		}

		fmt.Println("⏳ Saving changes to the server...")
		jsonData, _ := json.Marshal(c)
		req, _ := http.NewRequest(http.MethodPut, ServerURL+"/"+targetID, bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		putResp, err := client.Do(req)

		if err != nil {
			fmt.Println("❌ Error: Could not connect to the server.")
			continue
		}

		if putResp.StatusCode == http.StatusOK {
			var updatedC ClientCustomer
			json.NewDecoder(putResp.Body).Decode(&updatedC)
			putResp.Body.Close()

			c = updatedC

			fmt.Println("\n✅ Success! Customer profile has been updated perfectly.")
			fmt.Printf("ID: %s | Name: %s | Age: %d | Type: %s | Balance: $%.2f\n",
				updatedC.ID, updatedC.Name, updatedC.Age, updatedC.AccountType, updatedC.AccountBalance)

			fmt.Print("\n🔙 Press [ENTER] to continue updating this customer...")
			reader.ReadString('\n')

		} else {
			fmt.Println("❌ Failed to update customer on the server.")
			putResp.Body.Close()
		}
	}
}

func DeleteCustomer(reader *bufio.Reader) {
	targetID := promptInput(reader, ">> WARNING! Enter the Customer ID to DELETE: ")
	if targetID == "" {
		return
	}

	req, _ := http.NewRequest(http.MethodDelete, ServerURL+"/"+targetID, nil)
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		fmt.Println("❌ Error: Could not connect to the server.")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("\n✅ Success! Customer has been deleted.")
	} else if resp.StatusCode == http.StatusNotFound {
		fmt.Println("⚠️ Customer not found!")
	}
}
