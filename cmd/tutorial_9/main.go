package main

import (
	"fmt"
	"math/rand/v2"
	"time"
)

const MAX_CHICKEN_PRICE = 5.0

func main() {
	var c = make(chan string) // unbuffered channel

	websites := []string{"walmart.com", "costco.com", "wholefoods.com"}

	for i := range websites {
		go findDeal(websites[i], c) // start a goroutine for each website
	}

	sendMessage(c)
}

func findDeal(website string, chickenChannel chan string) {
	for {
		time.Sleep(time.Second * 1)
		chickenPrice := rand.Float32() * 20
		if chickenPrice < MAX_CHICKEN_PRICE {
			chickenChannel <- website // send the website through the channel. This will block until another goroutine receives the value.
		}
	}
}

func sendMessage(chickenChannel chan string) {
	fmt.Printf("Chicken deal found at %s!\n", <-chickenChannel) // blocks until a value is received
}
