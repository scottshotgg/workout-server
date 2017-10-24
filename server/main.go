package server

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	gocb "github.com/couchbase/gocb"
	"go.uber.org/zap"
)

var (
	logger  *zap.Logger
	err     error
	cluster *gocb.Cluster
	bucket  *gocb.Bucket
)

type Exercise struct {
	Reps   int `json:"reps"`
	Weight int `json:"weight"`
}

type Document struct {
	InsertionDate string                    `json:"insertion_date"`
	Date          string                    `json:"date"`
	Exercises     map[string]map[string]int `json:"exercises"`
}

func init() {
	logger, err = zap.NewDevelopment()
	if err != nil {
		os.Exit(77)
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		os.Exit(200)
	}

	fmt.Println(r.Method, string(body))

	docuBody := Document{}

	err = json.Unmarshal(body, &docuBody)
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}

	fmt.Printf("%+v", docuBody)

	docuBody.InsertionDate = strings.Split(time.Now().String(), " ")[0]

	//bucket.Upsert(key, value, expiry)
	thing := Document{}
	_, err = bucket.Get(docuBody.InsertionDate, &thing)
	if err != nil {
		logger.Info("yo wtf mayne")
		_, err = bucket.Upsert(docuBody.InsertionDate, docuBody, 0)
		if err != nil {
			logger.Info("yo wtf dawg")
		}
		return
	}
	fmt.Printf("%+v", thing)
	for exName, exer := range docuBody.Exercises {
		logger.Info(fmt.Sprintf("%+v", exer) + " " + exName)
		if thing.Exercises[exName] == nil {
			thing.Exercises[exName] = exer
		} else {
			for weight, reps := range exer {
				if thing.Exercises[exName][weight] == 0 {
					thing.Exercises[exName][weight] = reps
				} else {
					thing.Exercises[exName][weight] += reps
				}
			}
		}
	}

	fmt.Printf("%+v\n", thing)

	// right now just add that shit
	_, err = bucket.Upsert(docuBody.InsertionDate, thing, 0)
	if err != nil {
		logger.Info("yo wtf bro")
	}
}

func getLastTime(w http.ResponseWriter, r *http.Request) {
	value := Document{}
	_, err = bucket.Get(strings.Split(time.Now().AddDate(0, 0, -7).String(), " ")[0], &value)
	if err != nil {
		w.Write([]byte("Could not get last weeks data"))
		return
	}
	valueString, err := json.Marshal(value)
	if err != nil {
		w.Write([]byte("Could not marshal last weeks data"))
	}
	w.Write([]byte(valueString))
}

func startHTTP() {
	cluster, _ = gocb.Connect("couchbase://127.0.0.1")
	bucket, _ = cluster.OpenBucket("workout", "")

	http.HandleFunc("/lastweek", getLastTime)
	//http.HandleFunc("/today", getToday)
	http.HandleFunc("/", handler)
	http.ListenAndServe(":3000", nil)
}

// Start yeah man
func Start() error {
	startHTTP()

	return nil
}
