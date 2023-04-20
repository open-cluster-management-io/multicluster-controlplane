// Copyright Contributors to the Open Cluster Management project

package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	workv1 "open-cluster-management.io/api/work/v1"
)

const (
	expectedWorkCountAnnotation = "perftest.open-cluster-management.io/expected-work-count"
	defaultContent              = "I'm a test configmap"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(workv1.AddToScheme(genericScheme))
}

func PrintMsg(msg string) {
	now := time.Now()
	fmt.Fprintf(os.Stdout, "[%s] %s\n", now.Format(time.RFC3339), msg)
}

func AppendRecordToFile(filename, record string) error {
	if filename == "" {
		return nil
	}

	if record == "" {
		return nil
	}

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(fmt.Sprintf("%s\n", record)); err != nil {
		return err
	}
	return nil
}

func GenerateManifestWorks(workCount int, clusterName, templateDir string) ([]*workv1.ManifestWork, error) {
	if len(templateDir) == 0 {
		totalWorkloadSize := 0
		works := []*workv1.ManifestWork{}
		for i := 0; i < workCount; i++ {
			workName := fmt.Sprintf("perftest-%s-work-%d", clusterName, i)
			data := []byte(defaultContent)
			works = append(works, toManifestWork(clusterName, workName, data))
			totalWorkloadSize = totalWorkloadSize + len(data)
		}
		PrintMsg(fmt.Sprintf("Total workload size is %d bytes in cluster %s", totalWorkloadSize, clusterName))
		return works, nil
	}

	works, totalWorkloadSize, err := getManifestWorksFromTemplate(clusterName, templateDir)
	if err != nil {
		return nil, err
	}

	// if template works count is less than expected work count, continue to create default works
	for i := 0; i < workCount-len(works); i++ {
		workName := fmt.Sprintf("perftest-%s-work-%d", clusterName, i)
		data := []byte(defaultContent)
		works = append(works, toManifestWork(clusterName, workName, data))
		totalWorkloadSize = totalWorkloadSize + len(data)
	}
	PrintMsg(fmt.Sprintf("Total workload size is %d bytes in cluster %s", totalWorkloadSize, clusterName))
	return works, nil
}

func getManifestWorksFromTemplate(clusterName, templateDir string) ([]*workv1.ManifestWork, int, error) {
	files, err := os.ReadDir(templateDir)
	if err != nil {
		return nil, 0, err
	}

	totalWorkloadSize := 0
	works := []*workv1.ManifestWork{}
	for _, file := range files {
		info, err := file.Info()
		if err != nil {
			return nil, 0, err
		}

		if info.IsDir() {
			continue
		}

		if filepath.Ext(info.Name()) != ".yaml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(templateDir, info.Name()))
		if err != nil {
			return nil, 0, err
		}

		obj, _, err := genericCodec.Decode(data, nil, nil)
		if err != nil {
			return nil, 0, err
		}
		switch work := obj.(type) {
		case *workv1.ManifestWork:
			expectedWorkCountStr, ok := work.Annotations[expectedWorkCountAnnotation]
			if !ok {
				return nil, 0, fmt.Errorf("annotation %q is required", expectedWorkCountAnnotation)
			}

			expectedWorkCount, err := strconv.Atoi(expectedWorkCountStr)
			if err != nil {
				return nil, 0, err
			}

			for i := 0; i < expectedWorkCount; i++ {
				workName := fmt.Sprintf("perftest-%s-work-%s-%d", clusterName, work.Name, i)
				PrintMsg(fmt.Sprintf("work %s is created from template %s", workName, info.Name()))
				works = append(works, toManifestWork(clusterName, workName, data))
				totalWorkloadSize = totalWorkloadSize + len(data)
			}
		}
	}

	return works, totalWorkloadSize, nil
}

func generateManifest(workName string, data []byte) workv1.Manifest {
	manifest := workv1.Manifest{}
	manifest.Object = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      workName,
		},
		BinaryData: map[string][]byte{
			"test-data": data,
		},
	}
	return manifest
}

func toManifestWork(clusterName, workName string, data []byte) *workv1.ManifestWork {
	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: clusterName,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{
					generateManifest(workName, data),
				},
			},
		},
	}
}
