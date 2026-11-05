package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// config is what one run does. The defaults are the load target: 1,000
// principals, a calendar of 10,000 events, 50 recipients per shared
// calendar and 200 clients polling every 30 seconds.
type config struct {
	Binary     string
	DataDir    string
	KeepData   bool
	JSONOut    string
	Principals int
	// EventsPerUser is how many events each principal's own calendar holds.
	EventsPerUser int
	// LargeSizes are the sizes of the large calendars, one owner each.
	LargeSizes []int
	// Queries is how many range queries run against each large calendar.
	Queries     int
	Concurrency int
	// SharedCollections calendars owned by other principals are each shared
	// with RecipientsPer users. The first large calendar is shared with the
	// same number.
	SharedCollections int
	SharedEvents      int
	RecipientsPer     int
	// Clients poll with sync-collection every Interval for Duration.
	Clients    int
	Interval   time.Duration
	Duration   time.Duration
	WriteRate  float64 // events written per second during the storm
	BcryptCost int
}

func parseFlags(args []string) (config, error) {
	var c config
	var sizes string
	fs := flag.NewFlagSet("load", flag.ContinueOnError)
	fs.StringVar(&c.Binary, "bin", "", "path of the ocis-groupware binary; empty builds one from this module")
	fs.StringVar(&c.DataDir, "data", "", "data directory to create and fill; empty uses a temporary one")
	fs.BoolVar(&c.KeepData, "keep", false, "keep the data directory after the run")
	fs.StringVar(&c.JSONOut, "json", "", "also write the results as JSON to this file")
	fs.IntVar(&c.Principals, "principals", 1000, "number of principals")
	fs.IntVar(&c.EventsPerUser, "events-per-user", 10, "events in each principal's own calendar")
	fs.StringVar(&sizes, "sizes", "10000", "comma separated sizes of the large calendars")
	fs.IntVar(&c.Queries, "queries", 100, "range queries per large calendar")
	fs.IntVar(&c.Concurrency, "concurrency", 4, "parallel requests for the per-calendar measurements")
	fs.IntVar(&c.SharedCollections, "shared-collections", 20, "calendars shared with several users")
	fs.IntVar(&c.SharedEvents, "shared-events", 200, "events in each of those calendars")
	fs.IntVar(&c.RecipientsPer, "shares", 50, "recipients of each shared calendar")
	fs.IntVar(&c.Clients, "clients", 200, "clients in the sync storm")
	fs.DurationVar(&c.Interval, "interval", 30*time.Second, "time between the polls of one client")
	fs.DurationVar(&c.Duration, "duration", 2*time.Minute, "length of the sync storm")
	fs.Float64Var(&c.WriteRate, "write-rate", 2, "events written per second while the storm runs")
	fs.IntVar(&c.BcryptCost, "bcrypt-cost", 10, "bcrypt cost of the seeded app passwords")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	for _, s := range strings.Split(sizes, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 1 {
			return config{}, fmt.Errorf("-sizes: %q is not a positive number", s)
		}
		c.LargeSizes = append(c.LargeSizes, n)
	}
	return c, c.validate()
}

func (c config) validate() error {
	switch {
	case c.Principals < 1:
		return fmt.Errorf("-principals must be at least 1")
	case c.Clients > c.Principals:
		return fmt.Errorf("-clients (%d) cannot exceed -principals (%d)", c.Clients, c.Principals)
	case c.RecipientsPer >= c.Principals:
		return fmt.Errorf("-shares (%d) must be less than -principals (%d)", c.RecipientsPer, c.Principals)
	case c.Interval <= 0 || c.Duration <= 0:
		return fmt.Errorf("-interval and -duration must be positive")
	case c.Concurrency < 1:
		return fmt.Errorf("-concurrency must be at least 1")
	}
	return nil
}
