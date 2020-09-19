/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	cpaasv1alpha1 "github.com/goldmann/cpaas-operator/api/v1alpha1"
	"github.com/goldmann/cpaas-operator/controllers/utils"
	pipelinev1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuilderReconciler reconciles a Builder object
type BuilderReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	PodUtil  *utils.PodUtil
	HTTPUtil *utils.HTTPUtil
}

type TaskList struct {
	Tasks []string `yaml:"tasks"`
}

// Reconcile applies changes made to a Builder object
//
// +kubebuilder:rbac:groups=cpaas.redhat.com,resources=builders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cpaas.redhat.com,resources=builders/status,verbs=get;update;patch
func (r *BuilderReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Builder change detected")

	// Fetch Builder instance
	ctx := context.Background()
	builder := &cpaasv1alpha1.Builder{}
	err := r.Get(ctx, req.NamespacedName, builder)

	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected.
			// Return and don't requeue
			r.Log.Info("Builder resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object
		// Requeue the request
		r.Log.Error(err, "Failed to get Builder")
		return ctrl.Result{}, err
	}

	r.Log = r.Log.WithValues("Builder", builder.Name)

	r.Log.Info("Handing Builder change")

	r.PodUtil = &utils.PodUtil{
		Client: r.Client,
		Log:    r.Log.WithName("PodUtil"),
		Scheme: r.Scheme,
	}

	r.HTTPUtil = &utils.HTTPUtil{
		Log: r.Log.WithName("HTTPUtil"),
	}

	// Register all tasks found in the image
	err = r.registerTasks(builder)

	if err != nil {
		r.Log.Error(err, "Could not register tasks")
		return ctrl.Result{}, err
	}

	r.Log.Info("Builder handled")

	return ctrl.Result{}, nil
}

func (r *BuilderReconciler) readTasks(url string) ([]pipelinev1beta1.Task, error) {
	r.Log.Info("Reading tasks")

	taskList := TaskList{}

	var tasks []pipelinev1beta1.Task

	// TODO: Change this to get container port, not on via host
	// This is convenient at development time
	// Need to find better way to do this
	tasksYaml, err := r.HTTPUtil.Get(fmt.Sprintf("%s/tasks/tasks.yaml", url), 5*time.Second)

	if err != nil {
		r.Log.Error(err, "Could not read task list from remote host")
		return nil, err
	}

	// Reading the tasks.yaml and converting into an object
	// TODO: Schema?
	err = yaml.Unmarshal([]byte(tasksYaml), &taskList)

	if err != nil {
		r.Log.Error(err, "Reading task list failed")
		return nil, err
	}

	r.Log.Info("Found tasks", "Tasks", taskList.Tasks)

	for _, taskName := range taskList.Tasks {
		taskYaml, err := r.HTTPUtil.Get(fmt.Sprintf("%s/tasks/tekton/%s.yaml", url, taskName), 5*time.Second)

		if err != nil {
			r.Log.Error(err, "Could not read task from remote host", "Task", taskName)
			return nil, err
		}

		// Convert the task into Tekton's Task object
		dec := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(taskYaml)), 1000)
		task := &pipelinev1beta1.Task{}

		r.Log.Info("Decoding task", "Task.Name", taskName)

		if err := dec.Decode(&task); err != nil {
			r.Log.Error(err, "Could not decode Task", "Task.Name", taskName)
			return nil, err
		}

		r.Log.Info("Task decoded", "Task.Name", taskName)

		if taskName != task.Name {
			r.Log.Info("Mismatch between task name provided in tasks.yaml and actual name", "Task.Name.Provided", taskName, "Task.Name.Actual", task.Name)
		}

		tasks = append(tasks, *task)
	}

	return tasks, nil
}

func (r *BuilderReconciler) registerTasks(builder *cpaasv1alpha1.Builder) error {
	pod, err := r.PodUtil.CreatePod(builder)

	if err != nil {
		r.Log.Error(err, "Error while creating the Pod")
		return err
	}

	err = r.PodUtil.WaitForPod(pod, "Running")

	if err != nil {
		r.Log.Error(err, "Error while waiting for Pod")
		return err
	}

	// Give some time to nginx to boot
	// TODO: retry?
	time.Sleep(2 * time.Second)

	tasks, err := r.readTasks(fmt.Sprintf("http://%s:12345", pod.Status.HostIP))

	if err != nil {
		r.Log.Error(err, "Could not read tasks")
		return err
	}

	for _, task := range tasks {
		r.Log.Info("Registering task", "Task", task.Name)

		task.Namespace = pod.Namespace

		taskKey := types.NamespacedName{
			Name:      task.Name,
			Namespace: pod.Namespace,
		}

		ctx := context.Background()
		err := r.Get(ctx, taskKey, &task)

		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Task does not exist, creating", "Task.Namespace", task.Namespace, "Task.Name", task.Name)

				err := r.Create(ctx, &task)

				if err != nil {
					r.Log.Error(err, "Failed to create new Task", "Task.Namespace", task.Namespace, "Task.Name", task.Name)
					return err
				}
			} else {
				r.Log.Error(err, "Failed to get Task")
				return err

				//return ctrl.Result{}, err
			}
		} else {
			// Task already exists
			//What to do?
		}
	}

	r.PodUtil.DeletePod(pod)

	return nil
}

func (r *BuilderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cpaasv1alpha1.Builder{}).
		Complete(r)
}
