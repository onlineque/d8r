# d8r
d8r (downscaler for kubernetes)

This project implements downscaler for Kubernetes which is able to scale down deployments according to the pre-defined
schedule. To opt-in for the downscaling, the deployment needs to add following annotations:

    annotations:
      d8r/days: "Mon,Tue,Wed,Thu,Fri"
      d8r/startTime: "08:00 Europe/Prague"
      d8r/stopTime: "16:00 Europe/Prague"
      d8r/downTimeReplicas: "1"

- `d8r/days` specifies on which days of the week the downscaling/upscaling will be effective
- `d8r/startTime` defines the start time when the deployment should upscale to the original replicaset size
- `d8r/stopTime` defines the stop time when it should downscale to the size defined by d8r/downTimeReplicas

### TODO:

- add cronjobs and jobs suspending on schedule
