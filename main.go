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

const (
	Suspend Action = iota
	Resume
)

var actionName = map[Action]string{
	Upscale:   "upscale",
	Downscale: "downscale",
	NoAction:  "no action",
}

var jobActionName = map[Action]string{
	Suspend: "suspend",
	Resume:  "resume",
}

const annotationStartTime = "d8r/startTime"
const annotationStopTime = "d8r/stopTime"
const annotationTimeZone = "d8r/timeZone"
const annotationDownTimeReplicas = "d8r/downTimeReplicas"
const annotationOriginalReplicas = "d8r/originalReplicas"
const annotationDays = "d8r/days"

func Log(l *log.Logger, msg string) {
	l.SetPrefix(time.Now().Format("2006-01-02 15:04:05") + " ")
	l.Print(msg)
}

func getRidOfDate(t time.Time) (time.Time, error) {
	// now get rid of the date as it makes no sense to use it
	formattedTime := fmt.Sprintf("0000-Jan-01 %02d:%02d +0000 UTC",
		t.Hour(),
		t.Minute(),
	)

	resultTime, err := time.Parse("2006-Jan-02 15:04 -0700 MST", formattedTime)
	if err != nil {
		return time.Time{}, err
	}
	return resultTime, nil
}

func ConvertTimeNowToLocal(tz string) (time.Time, string, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, "", err
	}

	currentTime := time.Now().In(loc)
	weekday := currentTime.Weekday().String()[:3]

	result, err := getRidOfDate(currentTime)
	if err != nil {
		return time.Time{}, "", err
	}

	return result, weekday, nil
}

func ConvertAnnotationTime(t string, timeZone string) (time.Time, error) {
	loc, err := time.LoadLocation(timeZone)
	if err != nil {
		return time.Time{}, err
	}

	// get the timezone abbreviation and offset
	currentTime := time.Now().In(loc)
	zoneAbbreviation, zoneOffset := currentTime.Zone()
	// convert to +/-hours:minutes format
	zoneOffsetStr := fmt.Sprintf("%+.02d%.02d", zoneOffset/3600, zoneOffset%3600)

	formattedSrcTime := fmt.Sprintf("0000-Jan-01 %s %s %s", t, zoneOffsetStr, zoneAbbreviation)
	result, err := time.Parse("2006-Jan-02 15:04 -0700 MST", formattedSrcTime)

	if err != nil {
		return time.Time{}, err
	}

	return result, nil
}

func getDeploymentActionNeeded(annotations map[string]string, replicas int32, l *log.Logger) Action {
	timeZone := annotations[annotationTimeZone]

	// prepare the current time for comparison, convert to local time used in annotations
	timeNow, _, err := ConvertTimeNowToLocal(timeZone)
	if err != nil {
		Log(l, err.Error())
		return NoAction
	}

	startTime, startTimeOk := annotations[annotationStartTime]
	stopTime, stopTimeOk := annotations[annotationStopTime]
	if !startTimeOk || !stopTimeOk {
		// no d8r/startTime or d8r/stopTime annotation means this
		// deployment is not set up for d8r properly
		return NoAction
	}
	startTimeConv, err := ConvertAnnotationTime(startTime, timeZone)
	if err != nil {
		Log(l, err.Error())
		return NoAction
	}

	timeStartTime, err := getRidOfDate(startTimeConv)
	if err != nil {
		Log(l, err.Error())
		return NoAction
	}

	stopTimeConv, err := ConvertAnnotationTime(stopTime, timeZone)
	if err != nil {
		Log(l, err.Error())
		return NoAction
	}

	timeStopTime, err := getRidOfDate(stopTimeConv)
	if err != nil {
		Log(l, err.Error())
		return NoAction
	}

	fmt.Printf("now: %v, start: %v, stop: %v, timezone: %s\n", timeNow, timeStartTime, timeStopTime, timeZone)

	if timeStopTime.Before(timeNow) || timeStartTime.After(timeNow) {
		downTimeReplicas, err := strconv.ParseInt(annotations[annotationDownTimeReplicas], 10, 32)
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
		originalReplicas, err := strconv.ParseInt(annotations[annotationOriginalReplicas], 10, 32)
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

func isDeploymentActionNeeded(annotations map[string]string, replicas int32, l *log.Logger) bool {
	days, ok := annotations[annotationDays]
	if !ok {
		// no d8r/days annotation means this deployment is not set up for d8r properly
		return false
	}

	timeZone, timeZoneOk := annotations[annotationTimeZone]
	if !timeZoneOk {
		// no d8r/timeZone annotation means  this deployment is not set up for d8r properly
		return false
	}

	// abbreviation of the day today
	_, today, err := ConvertTimeNowToLocal(timeZone)
	if err != nil {
		Log(l, err.Error())
		return false
	}

	if !strings.Contains(days, today) {
		// no d8r/days schedule for today, so no action is needed
		return false
	}

	_, downTimeReplicasOk := annotations[annotationDownTimeReplicas]
	if !downTimeReplicasOk {
		// no d8r/downTimeReplicas annotation means this deployment is not set up for d8r properly
		return false
	}

	if getDeploymentActionNeeded(annotations, replicas, l) != NoAction {
		return true
	}
	return false
}

func checkDeployments(clientset *kubernetes.Clientset, l *log.Logger) {
	deployments, err := clientset.AppsV1().Deployments("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		Log(l, err.Error())
		os.Exit(3)
	}

	for _, deployment := range deployments.Items {
		annotations := deployment.Annotations
		actionNeeded := isDeploymentActionNeeded(annotations, *deployment.Spec.Replicas, l)
		if actionNeeded {
			actionToDo := getDeploymentActionNeeded(annotations, *deployment.Spec.Replicas, l)

			Log(l, fmt.Sprintf("deployment: %v/%v, replicas: %d, action needed: %v\n",
				deployment.Namespace,
				deployment.Name,
				*deployment.Spec.Replicas,
				actionName[actionToDo]))

			switch actionToDo {
			case Downscale:
				downTimeReplicas, err := strconv.ParseInt(deployment.Annotations[annotationDownTimeReplicas], 10, 32)
				if err != nil {
					Log(l, err.Error())
					continue
				}
				deployment.Annotations[annotationOriginalReplicas] = fmt.Sprintf("%d", *deployment.Spec.Replicas)
				downTimeReplicas32 := int32(downTimeReplicas)
				deployment.Spec.Replicas = &downTimeReplicas32
				deployment.SetAnnotations(deployment.Annotations)
			case Upscale:
				originalReplicas, err := strconv.ParseInt(deployment.Annotations[annotationOriginalReplicas], 10, 32)
				if err != nil {
					Log(l, err.Error())
					continue
				}
				originalReplicas32 := int32(originalReplicas)
				deployment.Spec.Replicas = &originalReplicas32
			}
			if actionToDo == Upscale || actionToDo == Downscale {
				// update the changed deployment
				_, err = clientset.AppsV1().Deployments(deployment.Namespace).Update(context.TODO(),
					&deployment,
					metav1.UpdateOptions{})
				if err != nil {
					Log(l, err.Error())
				}
			}
		}
	}
}

func getCronjobActionNeeded(annotations map[string]string, suspend bool, l *log.Logger) Action {
	// prepare the current time for comparison
	timeZone := annotations[annotationTimeZone]

	// prepare the current time for comparison, convert to local time used in annotations
	timeNow, _, err := ConvertTimeNowToLocal(timeZone)
	if err != nil {
		Log(l, err.Error())
		return NoAction
	}

	startTime, startTimeOk := annotations[annotationStartTime]
	stopTime, stopTimeOk := annotations[annotationStopTime]
	if !startTimeOk || !stopTimeOk {
		// no d8r/startTime or d8r/stopTime annotation means this
		// deployment is not set up for d8r properly
		return NoAction
	}
	startTimeConv, err := ConvertAnnotationTime(startTime, timeZone)
	if err != nil {
		Log(l, err.Error())
		return NoAction
	}

	timeStartTime, err := getRidOfDate(startTimeConv)
	if err != nil {
		Log(l, err.Error())
		return NoAction
	}

	stopTimeConv, err := ConvertAnnotationTime(stopTime, timeZone)
	if err != nil {
		Log(l, err.Error())
		return NoAction
	}

	timeStopTime, err := getRidOfDate(stopTimeConv)
	if err != nil {
		Log(l, err.Error())
		return NoAction
	}

	fmt.Printf("now: %v, start: %v, stop: %v, timezone: %s\n", timeNow, timeStartTime, timeStopTime, timeZone)

	// should be already stopped, time is over or should be still stopped, as uptime has not yet began
	if timeStopTime.Before(timeNow) || timeStartTime.After(timeNow) {
		// only suspend if not done yet
		if !suspend {
			return Suspend
		}
	}
	// should be already resumed but not yet suspended ( aka the uptime period)
	if timeStartTime.Before(timeNow) && !timeStopTime.Before(timeNow) {
		// only resume if not done yet
		if suspend {
			return Resume
		}
	}
	return NoAction
}

func isCronjobActionNeeded(annotations map[string]string, suspend bool, l *log.Logger) bool {
	days, ok := annotations[annotationDays]
	if !ok {
		// no d8r/days annotation means this deployment is not set up for d8r properly
		return false
	}

	timeZone, timeZoneOk := annotations[annotationTimeZone]
	if !timeZoneOk {
		// no d8r/timeZone annotation means  this deployment is not set up for d8r properly
		return false
	}

	// abbreviation of the day today
	_, today, err := ConvertTimeNowToLocal(timeZone)
	if err != nil {
		Log(l, err.Error())
		return false
	}

	if !strings.Contains(days, today) {
		// no d8r/days schedule for today, so no action is needed
		return false
	}

	if getCronjobActionNeeded(annotations, suspend, l) != NoAction {
		return true
	}
	return false
}

func checkCronjobs(clientset *kubernetes.Clientset, l *log.Logger) {
	cronjobs, err := clientset.BatchV1().CronJobs("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		Log(l, err.Error())
		os.Exit(3)
	}
	for _, cronjob := range cronjobs.Items {
		annotations := cronjob.Annotations
		suspend := cronjob.Spec.Suspend
		actionNeeded := isCronjobActionNeeded(annotations, *suspend, l)
		if actionNeeded {
			actionToDo := getCronjobActionNeeded(annotations, *suspend, l)
			Log(l, fmt.Sprintf("cronjob: %v/%v, action needed: %s\n",
				cronjob.Namespace,
				cronjob.Name,
				jobActionName[actionToDo]))
			var jobSuspend bool
			switch actionToDo {
			case Suspend:
				// suspend the job if it's downtime
				jobSuspend = true
				cronjob.Spec.Suspend = &jobSuspend
			case Resume:
				jobSuspend = false
				// otherwise resume
				cronjob.Spec.Suspend = &jobSuspend
			}
			if actionToDo == Suspend || actionToDo == Resume {
				_, err = clientset.BatchV1().CronJobs(cronjob.Namespace).Update(context.TODO(),
					&cronjob,
					metav1.UpdateOptions{})
				if err != nil {
					Log(l, err.Error())
				}
			}
		}
	}
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
		checkDeployments(clientset, l)
		checkCronjobs(clientset, l)
		time.Sleep(10 * time.Second)
	}
}
