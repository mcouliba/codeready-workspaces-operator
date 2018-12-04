//
// Copyright (c) 2012-2018 Red Hat, Inc.
// This program and the accompanying materials are made
// available under the terms of the Eclipse Public License 2.0
// which is available at https://www.eclipse.org/legal/epl-2.0/
//
// SPDX-License-Identifier: EPL-2.0
//
// Contributors:
//   Red Hat, Inc. - initial API and implementation
//
package util

import (
	"github.com/operator-framework/operator-sdk/pkg/k8sclient"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"log"
	"math/rand"
	"os"
)

// GetEnvValue looks for env variables in Operator pod to configure Code Ready deployments
// with things like db users, passwords and deployment options in general. Envs are set in
// a ConfigMap at deploy/config.yaml. Find more details on deployment options in README.md

func GetEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	return value
}

func GetEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	if value == "true" {
		return true
	}
	return false
}

func GeneratePasswd(stringLength int) (passwd string) {
	chars := []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"abcdefghijklmnopqrstuvwxyz" +
		"0123456789")
	length := stringLength
	buf := make([]rune, length)
	for i := range buf {
		buf[i] = chars[rand.Intn(len(chars))]
	}
	passwd = string(buf)
	return passwd
}

func GetInfra() (infra string) {
	kubeClient := k8sclient.GetKubeClient()
	serverGroups, _ := kubeClient.Discovery().ServerGroups()
	apiGroups := serverGroups.Groups

	for i := range apiGroups {
		name := apiGroups[i].Name
		if name == "route.openshift.io" {
			infra = "openshift"
		}
	}
	if infra == "" {
		infra = "kubernetes"
	}
	return infra
}

func GetNamespace() (currentNamespace string) {

	namespace, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		panic(err)
	}
	currentNamespace = string(namespace)
	return currentNamespace
}

func GetK8SConfig() (clientset *kubernetes.Clientset) {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
		return nil
	}
	return clientset
}

var (
	clientset = GetK8SConfig()
	timeout   = int64(300)
)

func GetDeploymentStatus(deployment *appsv1.Deployment) {
	api := clientset.AppsV1()
	listOptions := metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", deployment.Name).String(),
		TimeoutSeconds: &timeout,
	}
	_, err := api.Deployments(GetNamespace()).Get(deployment.Name, metav1.GetOptions{})
	if err != nil {
		logrus.Errorf("Failed to get %s deployment", deployment.Name)
		panic(err)
	}

	watcher, err := api.Deployments(GetNamespace()).Watch(listOptions)
	if err != nil {
		log.Fatal(err)
	}
	ch := watcher.ResultChan()
	logrus.Printf("Waiting for deployment %s. Default timeout is %v seconds", deployment.Name, timeout)

	for event := range ch {
		dc, ok := event.Object.(*appsv1.Deployment)
		if !ok {
			log.Fatal("Unexpected type")
		}

		switch event.Type {
		case watch.Modified:
			if dc.Status.AvailableReplicas == 1 {
				logrus.Infof("%s Successfully deployed", deployment.Name)
				watcher.Stop()
			}
			err := sdk.Get(deployment)
			if err != nil {
				logrus.Errorf("Failed to get %s deployment: %s", deployment.Name, err)
				panic(err)
			}
		}
	}
	if deployment.Status.AvailableReplicas != 1 {
		logrus.Errorf("Failed to verify a successful %s deployment. Operator is exiting", deployment.Name)
		logrus.Errorf("Get deployment logs: kubectl logs deployment/%s -n=%s", deployment.Name, deployment.Namespace)
		logrus.Errorf("Get k8s events: kubectl get events " +
			"--field-selector " +
			"involvedObject.name=$(kubectl get pods -l=app=%s -n=%s" +
			" -o=jsonpath='{.items[0].metadata.name}') -n=%s", deployment.Name, deployment.Namespace, deployment.Namespace)
		panic(err)
	}
}

func GetJobStatus(job *batchv1.Job) {
	api := clientset.BatchV1()
	listOptions := metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", job.Name).String(),

		TimeoutSeconds: &timeout,
	}
	_, err := api.Jobs(GetNamespace()).Get(job.Name, metav1.GetOptions{})
	if err != nil {
		logrus.Errorf("Failed to get %s job", job.Name)
		panic(err)
	}
	watcher, err := api.Jobs(GetNamespace()).Watch(listOptions)
	if err != nil {
		log.Fatal(err)
	}
	ch := watcher.ResultChan()
	logrus.Printf("Waiting for %s job. Default timeout is %v seconds", job.Name, timeout)

	for event := range ch {
		dbJob, ok := event.Object.(*batchv1.Job)
		if !ok {
			log.Fatal("Unexpected type")
		}

		switch event.Type {
		case watch.Modified:
			if dbJob.Status.Succeeded == 1 {
				logrus.Infof("%s job successfully executed", job.Name)
				watcher.Stop()
			}
			err := sdk.Get(job)
			if err != nil {
				logrus.Errorf("Failed to get %s job: %s", job.Name, err)
				panic(err)
			}
		}
	}
	if job.Status.Succeeded != 1 {
		logrus.Errorf("Failed to verify a successful %s job execution. Operator is exiting", job.Name)
		logrus.Errorf("Check job logs: kubectl logs job/%s -n=", job.Name, job.Namespace)
		logrus.Errorf("Get k8s events: kubectl get events " +
			"--field-selector involvedObject.name=$(kubectl get pods -l=app=%s -n=%s-o=jsonpath='{.items[0].metadata.name}') -n=",
			job.Name, job.Namespace, job.Namespace)
		panic(err)
	}
}
