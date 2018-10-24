package main

import (
	"fmt"
	"github.com/PagerDuty/go-pagerduty"
	"time"
	"flag"
	"os"
)

type onCall struct {
	engineer string
	minutes	 int64
	shift	 string
	override bool
}

type engineerOverview struct {
	engineer string
	shifts map[string][]int64
	override map[string][]int64
}

func parsePDTime(timeString string) (time.Time, error) {
	layout := "2006-01-02T15:04:05-07:00"
	time, err := time.Parse(layout, timeString)
	if err != nil {
		return time, err
	}
	return time, nil
}

func getScheduleId(c *pagerduty.Client, scheduleName string) (string, error) {
	var opts pagerduty.ListSchedulesOptions

	opts.Query = scheduleName

	if sched, err := c.ListSchedules(opts); err != nil {
		return "", error(err)
	} else {
		return sched.Schedules[0].ID, nil
	}
}

func getOncallEngineers(c *pagerduty.Client) ([]string, error){
	var opts pagerduty.ListUsersOptions

	var ocEngineers []string
	if resp, err := c.ListUsers(opts); err != nil {
		return nil, error(err)
	} else {
		for _, user := range resp.Users {
			ocEngineers = append(ocEngineers, user.Summary)
		}
		return ocEngineers, nil
	}
}

func initOnCallSummary(c *pagerduty.Client) map[string]engineerOverview {
	ocs := make(map[string]engineerOverview)

	engineers, err := getOncallEngineers(c)
	if err != nil {
		fmt.Errorf("Could not get List of Engineers")
		return ocs
	}

	for _, oce := range engineers  {
		var ovw engineerOverview
		ovw.engineer = oce
		ovw.shifts = make(map[string][]int64)
		ovw.override = make(map[string][]int64)
		ocs[oce] = ovw
	}
	return ocs
}

func getSchedule(c *pagerduty.Client, id string, since string, until string) (*pagerduty.Schedule, error) {
	var opts pagerduty.GetScheduleOptions

	opts.Since = since
	opts.Until = until

	if sched, err := c.GetSchedule(id ,opts); err != nil {
		fmt.Print(err)
		return nil, error(err)
	} else {
		return sched, nil
	}
}

func extractLayerShifts(c *pagerduty.Client, sched *pagerduty.Schedule, layerName string) []onCall {
	var ocl []onCall
	for _, l := range sched.ScheduleLayers {
		if l.Name == layerName {
			for _, fs := range sched.FinalSchedule.RenderedScheduleEntries {
				var entry onCall

				entry.engineer = fs.User.Summary
				entry.shift = layerName

				st, err := parsePDTime(fs.Start)
				if err != nil {
					fmt.Errorf("Could not parse Shift Start time!", err)
					continue
				}
				et, err := parsePDTime(fs.End)
				if err != nil {
					fmt.Errorf("Could not parse Shift Start time!", err)
					continue
				}

				duration := et.Sub(st)
				entry.minutes = int64(duration.Minutes())

				if len(sched.FinalSchedule.RenderedScheduleEntries) == 1 {
					entry.override = false
				} else {
					entry.override = true
				}

				ocl = append(ocl, entry)
			}
		}
	}
	return ocl
}

func main() {

	var authtoken = flag.String("token", "", "Auth Token for Pagerduty")
	var scheduleName = flag.String("name", "ACDC Oncall Schedule", "Name of the Oncall Schedule to extract")
	var startTime = flag.String("since", "2018-06-01T00:00:01", "Extract data since this timestamp in UTC")
	var endTime = flag.String("until", "2018-07-01T00:00:01", "Extract data until this timestamp in UTC")

	flag.Parse()

	if *authtoken == "" {
		flag.Usage()
		os.Exit(-1)
	}

	client := pagerduty.NewClient(*authtoken)

	id, _ := getScheduleId(client, *scheduleName)
	fmt.Printf("Extracting Schedule %s from %s to %s\n\n", id, *startTime, *endTime)

	var onCallList []onCall

	// Get the initial schedule
	sched, _ := getSchedule(client, id, *startTime, *endTime)

	// Iterate over all schedule layers to get the final schedule and the schedule layer name
	for _, sl := range sched.ScheduleLayers {
		for _, rse := range sl.RenderedScheduleEntries {
			// Get Schedule for the limited timeframe of the rendered Entry
			nsched, _ := getSchedule(client, id, rse.Start, rse.End)

			// Extract the Final Schedule for that specific layer
			ocl := extractLayerShifts(client, nsched, sl.Name)

			onCallList = append(onCallList,ocl...)
		}
	}

	onCallSummary := initOnCallSummary(client)

	for _, ocentry := range onCallList {
		if ocentry.override {
			onCallSummary[ocentry.engineer].override[ocentry.shift] =
				append(onCallSummary[ocentry.engineer].override[ocentry.shift], ocentry.minutes)
		} else {
			onCallSummary[ocentry.engineer].shifts[ocentry.shift] =
				append(onCallSummary[ocentry.engineer].shifts[ocentry.shift], ocentry.minutes)
		}

	}

	for user, eoverview := range onCallSummary {
		fmt.Printf("Engineer: %s\n", user )

		for shiftname, minuteArray := range eoverview.shifts {
			numOverrides := len(eoverview.override[shiftname])
			num := len(eoverview.shifts[shiftname])
			var acc int64
			for _, v := range minuteArray {
				acc = acc + v
			}
			fmt.Printf("%s: %d h %d min in %d shift(s) and %d override(s)\n", shiftname, acc/60, acc%60, num, numOverrides)
		}
		fmt.Println()
	}

}
