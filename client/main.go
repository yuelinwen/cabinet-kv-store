package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yuelinwen/cabinet-kv-store/client/router"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("⏳ Attempting to connect to backend server at %s...\n", router.ServerURL)
	client := http.Client{Timeout: 2 * time.Second}
	_, err := client.Get(router.ServerURL)

	if err != nil {
		fmt.Println("\n❌ [FATAL ERROR] Connection Failed!")
		fmt.Println(">> Reason:", err.Error())
		fmt.Println(">> Please make sure your server is running (go run main.go -port 8080).")
		os.Exit(1)
	}

	fmt.Println("✅ Connection successful!\n")
	time.Sleep(500 * time.Millisecond)

	fmt.Println("=======================================================")
	fmt.Println("       🏛️  WELCOME TO CABINET BANKING SYSTEM  🏛️       ")
	fmt.Println("=======================================================")

	for {
		fmt.Println("\n---------------------- MAIN MENU ----------------------")
		fmt.Println("[1] 🏦 View All Customers")
		fmt.Println("[2] 🔍 Find Customer by ID")
		fmt.Println("[3] ➕ Open New Account")
		fmt.Println("[4] ✏️ Update Customer Info")
		fmt.Println("[5] ❌ Close Account")
		fmt.Println("[0] 🚪 Exit System")
		fmt.Println("-------------------------------------------------------")

		fmt.Print("👉 Please enter your choice (0-5): ")
		input, _ := reader.ReadString('\n')
		choice := strings.TrimSpace(input)

		fmt.Println("\n-------------------------------------------------------")

		switch choice {
		case "1":
			router.ViewAllCustomers()
			router.WaitForEnter(reader)

		case "2":
			router.SearchCustomer(reader)
			router.WaitForEnter(reader)

		case "3":
			router.CreateCustomer(reader)
			router.WaitForEnter(reader)

		case "4":
			router.UpdateCustomer(reader)
			router.WaitForEnter(reader)

		case "5":
			router.DeleteCustomer(reader)
			router.WaitForEnter(reader)

		case "0":
			fmt.Println("👋 Thank you for using the Banking System. Goodbye!")
			fmt.Println("=======================================================")
			return

		default:
			fmt.Println("❌ Invalid input! Please enter a number between 0 and 5.")
			router.WaitForEnter(reader)
		}
	}
}
