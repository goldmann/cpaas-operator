package utils

import (
	"context"
	"errors"
	"time"

	"github.com/go-logr/logr"
	cpaasv1alpha1 "github.com/goldmann/cpaas-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	kuberneteserrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodUtil struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (util *PodUtil) CreatePod(builder *cpaasv1alpha1.Builder) (*corev1.Pod, error) {
	pod := util.podDefinition(builder)

	podKey := types.NamespacedName{
		Name:      pod.Name,
		Namespace: pod.Namespace,
	}

	ctx := context.Background()
	err := util.Client.Get(ctx, podKey, pod)

	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			util.Log.Error(err, "Failed to check if Pod exists")
			return nil, err
		}
	} else {
		// Ooops. It looks like the pod already exists which is unexpected.
		// We need to delete it before we can continue.
		util.Log.Info("Pod already running, deleting it", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)

		util.DeletePod(pod)
	}

	// All good
	util.Log.Info("Creating pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)

	err = util.Client.Create(ctx, pod)

	if err != nil {
		util.Log.Error(err, "Failed to create new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
		return nil, err
	}

	return pod, nil
}

func (util *PodUtil) DeletePod(pod *corev1.Pod) {
	ctx := context.Background()
	err := util.Client.Delete(ctx, pod)

	if err != nil {
		util.Log.Error(err, "Failed to delete Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
	}

	util.Log.Info("Pod deleted")
}

// GetPod updates information about specific Pod
func (util *PodUtil) GetPod(pod *corev1.Pod) *corev1.Pod {
	podKey := types.NamespacedName{
		Name:      pod.Name,
		Namespace: pod.Namespace,
	}

	ctx := context.Background()
	err := util.Client.Get(ctx, podKey, pod)

	if err != nil {
		util.Log.Info("ERROR while checking pod, sleeping for 2 sec")
		time.Sleep(2 * time.Second)
		pod = util.GetPod(pod)
	}

	return pod
}

// WaitForPod waits for the specified Pod to be in the desired phase
// Times out after a minute
func (util *PodUtil) WaitForPod(pod *corev1.Pod, phase corev1.PodPhase) error {
	finished := make(chan bool)
	quit := make(chan bool)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		for {
			select {
			case <-quit:
				return
			default:
				pod = util.GetPod(pod)

				if pod.Status.Phase == phase {
					finished <- true
					return
				}

				util.Log.Info("Pod is not in requested phase, retrying...", "Pod.CurrentPhase", pod.Status.Phase, "Pod.RequestedPhase", phase)
				time.Sleep(1 * time.Second)
			}
		}

	}()

	select {
	case <-ctx.Done():
		quit <- true
		return errors.New("Timeout while waiting for Pod to come up")
	case <-finished:
		util.Log.Info("Pod in requested phase", "Pod.CurrentPhase", pod.Status.Phase, "Pod.RequestedPhase", phase)
	}

	return nil
}

func (util *PodUtil) podDefinition(b *cpaasv1alpha1.Builder) *corev1.Pod {
	labels := map[string]string{"app": "cpaas", "builder": b.Name}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.Name,
			Namespace: b.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Image:   b.Spec.Image,
				Name:    "task",
				Command: []string{"cp", "-ra", "/tasks", "/data"},
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/data",
					Name:      "data",
				}},
			}},
			Containers: []corev1.Container{{
				Image: "nginx:stable-alpine",
				Name:  "nginx",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/usr/share/nginx/html",
					Name:      "data",
					ReadOnly:  true,
				}},
				Ports: []corev1.ContainerPort{{
					// TODO: Change this!
					HostPort:      12345,
					ContainerPort: 80,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			}},
		},
	}

	ctrl.SetControllerReference(b, pod, util.Scheme)

	return pod
}
