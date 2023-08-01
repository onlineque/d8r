# d8r
d8r (downscaler for kubernetes)

This project implements downscaler for Kubernetes which is able to scale down deployments according to the pre-defined
schedule. To opt-in for the downscaling, the deployment needs to add following annotations:

    annotations:
      d8r/days: "Mon,Tue,Wed,Thu,Fri"
      d8r/startTime: "08:00"
      d8r/stopTime: "16:00"
      d8r/timeZone: "Pacific/Samoa"
      d8r/downTimeReplicas: "1"

- `d8r/days` specifies on which days of the week the downscaling/upscaling will be effective
- `d8r/startTime` defines the start time when the deployment should upscale to the original replicaset size
- `d8r/stopTime` defines the stop time when it should downscale to the size defined by `d8r/downTimeReplicas`
- `d8r/timeZone` defines the timezone used to define startTime and stopTime annotations
- `d8r/downTimeReplicas` specified the replica count used during the downtime, 0 means the deployment will be stopped completely

Annotations used for cronjobs:

    annotations:
      d8r/days: "Mon,Tue,Wed,Thu,Fri"
      d8r/startTime: "08:00"
      d8r/stopTime: "16:00"
      d8r/timeZone: "Pacific/Samoa"

- `d8r/days` specifies on which days of the week the downscaling/upscaling will be effective
- `d8r/startTime` defines the start time when the deployment should upscale to the original replicaset size
- `d8r/stopTime` defines the stop time when it should downscale to the size defined by `d8r/downTimeReplicas`
- `d8r/timeZone` defines the timezone used to define startTime and stopTime annotations

Notice: Cronjobs are suspended during the pre-defined downtime.

### TODO:

- add jobs suspending on schedule
