package main

import (
	"os"

	"product-collection-form/backend/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:]))
}
