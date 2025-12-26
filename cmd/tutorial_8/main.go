package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

var dbData = []string{"id1", "id2", "id3", "id4", "id5"}
var results = []string{}

func main() {
	// concurrency != parrallelism. We can have concurrency without parallelism (one core context switching). We can also have conrurrency with parallelism (multiple cores)

	// Go routines basically spawn a new thread to run a function concurrently

	// if shared memory is being accessed by multiple goroutines, we need to use sync mechanisms like mutexes to avoid race conditions

	// sequential()

	// goRoutines()

	// goRoutinesWithWaitGroup()

	goRoutinesWithWaitAndMutex()

}

func sequential() {
	// this implementation does not use go routines
	// each database call is made one after another, blocking the main function until each call returns
	// this is inefficient as we are not utilizing the waiting time of each database call to do other work
	// total time taken will be the sum of all database call times
	t0 := time.Now()

	for i := 0; i < 5; i++ {
		dbCall(i)
	}
	t1 := time.Now()
	fmt.Println("Time taken without goroutines: ", t1.Sub(t0))
	fmt.Println("Results: ", results)
}

func goRoutines() {
	// this implementation uses go routines, however it does NOT wait for the results to come back before exiting the main function
	t2 := time.Now()

	for i := 0; i < 5; i++ {
		go dbCall(i)
	}

	t3 := time.Now()
	fmt.Println("Time taken with go routines: ", t3.Sub(t2))
	fmt.Println("Results: ", results)
}

var wg = sync.WaitGroup{}

func goRoutinesWithWaitGroup() {
	// this implementation uses go routines along with a wait group to wait for all goroutines to finish before exiting the main function
	t4 := time.Now()

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go dbCallWithWaitGroup(i)
	}
	wg.Wait()
	t5 := time.Now()
	fmt.Println("Time taken with go routines and wait group: ", t5.Sub(t4))
	fmt.Println("Results: ", results)
}

// var m = sync.Mutex{}
var m = sync.RWMutex{}

func goRoutinesWithWaitAndMutex() {
	// this implementation uses go routines along with a wait group and a mutex to protect shared memory access
	t5 := time.Now()

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go dbCallWithWaitAndMutex(i)
	}
	wg.Wait()
	t6 := time.Now()
	fmt.Println("Time taken with go routines and wait group: ", t6.Sub(t5))
	fmt.Println("Results: ", results)
}

func dbCall(i int) {
	// simulate a database call
	delay := rand.Float32() * 2000
	time.Sleep(time.Duration(delay) * time.Millisecond)
	fmt.Println("The result from the database is: ", dbData[i])
	results = append(results, dbData[i])
}

func dbCallWithWaitGroup(i int) {
	// simulate a database call
	delay := 2000
	time.Sleep(time.Duration(delay) * time.Millisecond)
	fmt.Println("The result from the database is: ", dbData[i])
	results = append(results, dbData[i])
	wg.Done()
}

func dbCallWithWaitAndMutex(i int) {
	// simulate a database call
	delay := 2000
	time.Sleep(time.Duration(delay) * time.Millisecond)
	log()
	m.Lock()
	results = append(results, dbData[i])
	m.Unlock()
	wg.Done()
}

func log() {
	// many go routines can acquire a read lock. If a write lock is requested, it waits until all read locks are released.
	m.RLock() // checks for a full lock on the mutex. if exists, wait until its released.
	fmt.Println("Current results: ", results)
	m.RUnlock()
}
