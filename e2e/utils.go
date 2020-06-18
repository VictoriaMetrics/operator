package e2e

import (
	"errors"
	"fmt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"testing"
	"time"
)

var (
	NeedToWaitError = fmt.Errorf("needToWaitErr")
)

type waitForFunc func() error

func WaitForService(t *testing.T, kubeclient kubernetes.Interface, namespace, name string,
	retryInterval, timeout time.Duration) error {
	waitForService := func()error{
		_, err := kubeclient.CoreV1().Services(namespace).Get(name, metav1.GetOptions{})
		return err
	}
	t.Logf("Service available (%s)\n", name)
	return waitForEntity(retryInterval,timeout,waitForService)
}

func WaitForConfigMap(t *testing.T, kubeclient kubernetes.Interface, namespace, name string,
	retryInterval, timeout time.Duration) error {
	waitForCm := func()error {
		_, err := kubeclient.CoreV1().ConfigMaps(namespace).Get(name, metav1.GetOptions{})
		return  err
	}
	t.Logf("StatefulSet available %s\n", name)
	return waitForEntity(retryInterval,timeout,waitForCm)
}

func WaitForSecret(t *testing.T, kubeclient kubernetes.Interface, namespace, name string,
	retryInterval, timeout time.Duration) error {

	waitForSecret := func() error {

		_, err := kubeclient.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
		return err
	}
	t.Logf("Secret available %s\n", name)
	return waitForEntity(retryInterval, timeout, waitForSecret)
}

func WaitForSts(t *testing.T, kubeclient kubernetes.Interface, namespace, name string, replicas int, retryInterval, timeout time.Duration) error {
	waitForSts := func() error {
		sts, err := kubeclient.AppsV1().StatefulSets(namespace).Get(name, metav1.GetOptions{})
		if sts != nil {
			if int(sts.Status.ReadyReplicas) < replicas {
				t.Logf("Waiting for availability of Sts: %s in Namespace: %s \n", name, namespace)
				return NeedToWaitError
			}
		}
		return err
	}
	t.Logf("Sts available %s\n", name)
	return waitForEntity(retryInterval, timeout, waitForSts)
}

func waitForEntity(retryInterval, timeout time.Duration, fn waitForFunc) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		err = fn()
		if err != nil {
			if apierrors.IsNotFound(err) || errors.Is(err, NeedToWaitError) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	return nil
}
