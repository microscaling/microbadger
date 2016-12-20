// Example use of MicroBadger APIs
// For more information please see microbadger.com/api
// (c) 2016 Microscaling Systems Ltd
package main

import (
	"fmt"
	"os"

	"github.com/microscaling/microbadger/api"
)

func main() {
	labels, err := api.GetLabels("microscaling/microscaling")
	if err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}

	fmt.Println("Image microscaling/microscaling has these labels:")
	fmt.Println()
	for key, val := range labels {
		fmt.Printf("  %s: %s\n", key, val)
	}
	fmt.Println()
}
