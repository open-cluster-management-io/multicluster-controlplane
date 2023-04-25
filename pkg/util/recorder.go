// Copyright Contributors to the Open Cluster Management project
package util

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/klog/v2"
)

type loggingRecorder struct {
	componentName string
}

var _ events.Recorder = &loggingRecorder{}

func NewLoggingRecorder(componentName string) events.Recorder {
	return &loggingRecorder{componentName: componentName}
}

func (r *loggingRecorder) Event(reason, message string) {
	klog.Infof("component=%s, reason=%s, msg=%s", r.componentName, reason, message)
}

func (r *loggingRecorder) Eventf(reason, messageFmt string, args ...interface{}) {
	r.Event(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *loggingRecorder) Warning(reason, message string) {
	klog.Warningf("component=%s, reason=%s, msg=%s", r.componentName, reason, message)
}

func (r *loggingRecorder) Warningf(reason, messageFmt string, args ...interface{}) {
	r.Warning(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *loggingRecorder) ForComponent(componentName string) events.Recorder {
	newRecorder := *r
	newRecorder.componentName = componentName
	return &newRecorder
}

func (r *loggingRecorder) WithComponentSuffix(componentNameSuffix string) events.Recorder {
	return r.ForComponent(fmt.Sprintf("%s-%s", r.ComponentName(), componentNameSuffix))
}

func (r *loggingRecorder) WithContext(ctx context.Context) events.Recorder {
	return r
}

func (r *loggingRecorder) ComponentName() string {
	return r.componentName
}

func (r *loggingRecorder) Shutdown() {}
