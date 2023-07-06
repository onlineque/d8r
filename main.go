package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Action int

const (
	Upscale Action = iota
	Downscale
	NoAction
)

var actionName = map[Action]string{
	Upscale:   "upscale",
	Downscale: "downscale",
	NoAction:  "no action",
}

func Log(l *log.Logger, msg string) {
	l.SetPrefix(time.Now().Format("2006-01-02 15:04:05") + " ")
	l.Print(msg)
}

func ConvertTimeToUTC(t string) (time.Time, error) {
	timeSplit := strings.Split(t, " ")
	if len(timeSplit) != 2 {
		return time.Time{}, fmt.Errorf("invalid string: %s", t)
	}
	// load location to get TZ abbreviation and zone offset
	loc, err := time.LoadLocation(timeSplit[1])
	if err != nil {
		return time.Time{}, err
	}
	currentTime := time.Now().In(loc)

	zoneAbbreviation, zoneOffset := currentTime.Zone()
	// convert to +/-hours:minutes format
	zoneOffsetStr := fmt.Sprintf("%+.02d%.02d", zoneOffset/3600, zoneOffset%3600)

	today := time.Now()

	formattedSrcTime := fmt.Sprintf("%d-%s-%02d %s %s %s",
		today.Year(),
		today.Month().String()[:3],
		today.Day(),
		timeSplit[0],
		zoneOffsetStr,
		zoneAbbreviation)

	// finally convert the time to normalized format
	normalizedTime, err := time.Parse("2006-Jan-02 15:04 -0700 MST", formattedSrcTime)
	if err != nil {
		return time.Time{}, err
	}

	// convert to UTC
	UTCLocation, err := time.LoadLocation("UTC")
	if err != nil {
		return time.Time{}, err
	}
	convertedTime := normalizedTime.In(UTCLocation)

	return convertedTime, nil
}

func getActionNeeded(annotations map[string]string, replicas int32, l *log.Logger) Action {
	// prepare the current time for comparison
	timeNow := time.Now()
	//timeToConvert := fmt.Sprintf("%02d:%02d", now.Hour(), now.Minute())
	//timeNow, err := time.Parse("15:04", timeToConvert)
	//if err != nil {
	//	Log(l, err.Error())
	//	return NoAction
	//}

	startTime, startTimeOk := annotations["d8r/startTime"]
	stopTime, stopTimeOk := annotations["d8r/stopTime"]
	if !startTimeOk || !stopTimeOk {
		// no d8r/startTime or d8r/stopTime annotation means this
		// deployment is not set up for d8r properly
		return NoAction
	}
	timeStartTime, err := ConvertTimeToUTC(startTime)
	if err != nil {
		Log(l, err.Error())
		return NoAction
	}
	timeStopTime, err := ConvertTimeToUTC(stopTime)
	if err != nil {
		Log(l, err.Error())
		return NoAction
	}

	fmt.Printf("now: %v, start: %v, stop: %v\n", timeNow, timeStartTime, timeStopTime)

	if timeStopTime.Before(timeNow) {
		downTimeReplicas, err := strconv.ParseInt(annotations["d8r/downTimeReplicas"], 10, 32)
		if err != nil {
			Log(l, err.Error())
			return NoAction
		}
		// only downscale if not done yet
		if replicas != int32(downTimeReplicas) {
			return Downscale
		}
	}
	if timeStartTime.Before(timeNow) && !timeStopTime.Before(timeNow) {
		originalReplicas, err := strconv.ParseInt(annotations["d8r/originalReplicas"], 10, 32)
		if err != nil {
			Log(l, err.Error())
			return NoAction
		}
		// only upscale if not done yet
		if replicas != int32(originalReplicas) {
			return Upscale
		}
	}
	return NoAction
}

func isActionNeeded(annotations map[string]string, replicas int32, l *log.Logger) bool {
	days, ok := annotations["d8r/days"]
	if !ok {
		// no d8r/days annotation means this deployment is not set up for d8r properly
		return false
	}
	// abbreviation of the day today
	today := time.Now().Weekday().String()[:3]
	if !strings.Contains(days, today) {
		// no d8r/days schedule for today, so no action is needed
		return false
	}

	_, downTimeReplicasOk := annotations["d8r/downTimeReplicas"]
	if !downTimeReplicasOk {
		// no d8r/downTimeReplicas annotation means this deployment is not set up for d8r properly
		return false
	}

	if getActionNeeded(annotations, replicas, l) != NoAction {
		return true
	}
	return false
}

func main() {
	l := log.New(os.Stdout, "", 0)

	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		Log(l, err.Error())
		os.Exit(1)
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		Log(l, err.Error())
		os.Exit(2)
	}
	for {
		deployments, err := clientset.AppsV1().Deployments("").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			Log(l, err.Error())
			os.Exit(3)
		}

		Log(l, fmt.Sprintf("There are %d deployments in the cluster\n", len(deployments.Items)))

		for _, deployment := range deployments.Items {
			annotations := deployment.Annotations
			actionNeeded := isActionNeeded(annotations, *deployment.Spec.Replicas, l)
			if actionNeeded {
				actionToDo := getActionNeeded(annotations, *deployment.Spec.Replicas, l)

				Log(l, fmt.Sprintf("Name: %v, replicas: %d, action needed: %v\n",
					deployment.Name,
					*deployment.Spec.Replicas,
					actionName[actionToDo]))

				switch actionToDo {
				case Downscale:
					downTimeReplicas, err := strconv.ParseInt(deployment.Annotations["d8r/downTimeReplicas"], 10, 32)
					if err != nil {
						Log(l, err.Error())
						continue
					}
					deployment.Annotations["d8r/originalReplicas"] = fmt.Sprintf("%d", *deployment.Spec.Replicas)
					downTimeReplicas32 := int32(downTimeReplicas)
					deployment.Spec.Replicas = &downTimeReplicas32
					deployment.SetAnnotations(deployment.Annotations)
					_, err = clientset.AppsV1().Deployments(deployment.Namespace).Update(context.TODO(),
						&deployment,
						metav1.UpdateOptions{})
					if err != nil {
						Log(l, err.Error())
					}
				case Upscale:
					originalReplicas, err := strconv.ParseInt(deployment.Annotations["d8r/originalReplicas"], 10, 32)
					if err != nil {
						Log(l, err.Error())
						continue
					}
					originalReplicas32 := int32(originalReplicas)
					deployment.Spec.Replicas = &originalReplicas32
					_, err = clientset.AppsV1().Deployments(deployment.Namespace).Update(context.TODO(),
						&deployment,
						metav1.UpdateOptions{})
					if err != nil {
						Log(l, err.Error())
					}
				}
			}
		}

		time.Sleep(10 * time.Second)
	}
}
