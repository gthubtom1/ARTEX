package main

import (
	"fmt"
	"os"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
)

func main() {
	dsn := os.Getenv("ARTEX_PG_DSN")
	d, err := db.Open(dsn)
	if err != nil { panic(err) }
	defer d.Close()
	p2, err := agent.NewP2(d, "")
	if err != nil { panic(err) }
	agent.SetDefaultP2(p2)
	ts := agent.NewToolSet(nil, "work#1")
	ts.SetTaskID(1)
	fmt.Println("workerP2 =", len(ts.P2WorkerTools()))
	fmt.Println("workerDeferred =", len(ts.P2WorkerDeferred()))
	fmt.Println("plannerP2 =", len(ts.P2PlannerTools()))
	fmt.Println("plannerDeferred =", len(ts.P2PlannerDeferred()))
}
