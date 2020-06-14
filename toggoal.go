package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gen2brain/beeep"
	"gopkg.in/yaml.v2"
)

func getTotalGrand(url string, basicAuth string) float64 {
	method := "GET"
	client := &http.Client{}
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		fmt.Println(err)
	}
	req.Header.Add("Authorization", basicAuth)
	res, err := client.Do(req)
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)

	var data map[string]interface{}
	json.Unmarshal(body, &data)

	totalGrand := data["total_grand"].(float64) / 1000 / 60 / 60
	return totalGrand
}

func getCurrent(projects []int, basicAuth string) float64 {
	url := "https://www.toggl.com/api/v8/time_entries/current"
	method := "GET"
	client := &http.Client{}
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		fmt.Println(err)
	}
	req.Header.Add("Authorization", basicAuth)
	res, err := client.Do(req)
	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()

	var data map[string]interface{}
	json.Unmarshal(body, &data)

	if data["data"] == nil {
		return 0.
	}
	data = data["data"].(map[string]interface{})
	project := int(data["pid"].(float64))
	for _, projectInSlice := range projects {
		if project == projectInSlice {
			return (float64(time.Now().Unix()) + data["duration"].(float64)) / 60 / 60
		}
	}
	return 0.
}

type IBM struct {
	Client      int     `yaml:client`
	DefaultGoal float64 `yaml:"defaultGoal"`
	Projects    []int   `yaml:"projects"`
}

type PP struct {
	DefaultGoal float64 `yaml:"defaultGoal"`
	Project     int     `yaml:project`
}

type SP struct {
	DefaultGoal float64 `yaml:"defaultGoal"`
	Projects    []int   `yaml:"projects"`
}

type Config struct {
	DefaultTimeout   float64 `yaml:"defaultTimeout"`
	DefaultWorkspace string  `yaml:"defaultWorkspace"`
	BasicAuth        string  `yaml:"basicAuth"`
	Workspaces       struct {
		IBM IBM
		PP  PP
		SP  SP
	}
}

func main() {
	file, err := ioutil.ReadFile("config.yml")
	if err != nil {
		panic(err)
	}
	var config Config
	yaml.Unmarshal(file, &config)

	help := flag.Bool("h", false, "Help")
	splitGoal := flag.Bool("s", false, "Decide whether goal should be split to for weekday or not")
	goal := flag.Float64("g", 0., "Goal in hours")
	// skipDays := flag.String("d", "", "Work days skipped for goal splitting. Format e.g.: `MoThWe`") F also with holidays
	timeout := flag.Float64("t", config.DefaultTimeout, "Timeout in hours")
	workspaceName := flag.String("w", config.DefaultWorkspace, "Workspace defined in config.yml")
	flag.Parse()
	if *help {
		flag.PrintDefaults()
		os.Exit(0)
	}

	var client int
	var projects []int
	switch *workspaceName {
	case "ibm":
		wsConf := config.Workspaces.IBM
		client = wsConf.Client
		projects = wsConf.Projects
		if *goal == 0. {
			*goal = wsConf.DefaultGoal
		}
	case "pp":
		wsConf := config.Workspaces.PP
		projects = []int{wsConf.Project}
		if *goal == 0. {
			*goal = wsConf.DefaultGoal
		}
	case "sp":
		wsConf := config.Workspaces.SP
		projects = wsConf.Projects
		if *goal == 0. {
			*goal = wsConf.DefaultGoal
		}
	}

	basicAuth := fmt.Sprintf("Basic %s", config.BasicAuth)
	sleepMinutes, _ := time.ParseDuration(fmt.Sprintf("%fh", *timeout))

	now := time.Now()
	weekStart := now // retrieve date of last Monday
	for weekStart.Weekday() != time.Monday {
		weekStart = weekStart.AddDate(0, 0, -1)
	}
	url := "https://toggl.com/reports/api/v2/weekly?workspace_id=4176066&user_agent=t1m4nn@gmail.com&since=" + weekStart.Format("2006-01-02") // format REST url
	if client != 0 {
		url = url + fmt.Sprintf("&client_ids=%d", client)
	} else {
		url = url + fmt.Sprintf("&project_ids=%d", projects[0])
	}

	var current, total float64
	var countingDays float64
	var dayGoal float64
	var nextWorkday time.Time
	for { // for not skipped workdays F
		if *splitGoal {
			countingDays = float64(now.Truncate(24*time.Hour).Sub(weekStart.Truncate(24*time.Hour)))/60/60/24/1000000000 + 1
			dayGoal = *goal * countingDays / 5
		} else {
			dayGoal = *goal
		}

		current = getCurrent(projects, basicAuth)
		total = getTotalGrand(url, basicAuth) + current

		timein := time.Duration((dayGoal-total-*timeout)*60) * time.Minute
		log.Printf(" Sleeping for %v  Goal:%.2fh Total:%.2fh Running:%t", timein, dayGoal, total, current != 0)
		time.Sleep(timein)

		for total < dayGoal-*timeout {
			log.Printf("Sleeping for %v  Goal:%.2fh Total:%.2fh Running:%t", sleepMinutes, dayGoal, total, current != 0)
			time.Sleep(sleepMinutes)
			current := getCurrent(projects, basicAuth)
			total = getTotalGrand(url, basicAuth) + current
		}

		err = beeep.Alert("Works done!", fmt.Sprintf("%.2fh are reached in %v", dayGoal, sleepMinutes), "assets/warning.png") // notify when goal is attained
		if err != nil {
			panic(err)
		}
		fmt.Printf("Done with %.2fh by a total of %.2fh\n", dayGoal, total)

		if !*splitGoal {
			os.Exit(0)
		}

		yyyy, mm, dd := now.Date()
		goalTime, _ := time.ParseDuration(fmt.Sprintf("%fh", dayGoal/countingDays))
		goalSeconds := int(goalTime.Seconds())
		if now.Weekday() != time.Friday { // wait till the next workday (7am + difference)
			nextWorkday = time.Date(yyyy, mm, dd+1, 7, 15, 0+goalSeconds, 0, now.Location())
		} else {
			nextWorkday = time.Date(yyyy, mm, dd+3, 7, 15, 0+goalSeconds, 0, now.Location())
		}
		fmt.Printf("Waiting until %v\n", nextWorkday.Format("Mon Jan 2 15:04"))
		time.Sleep(time.Until(nextWorkday))
		now = time.Now()
	}

}
