package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

var (
	errAlreadyReplaced = fmt.Errorf("image is already replaced for this object")

	// ENV variables
	username = os.Getenv("DOCKER_USERNAME")
	password = os.Getenv("DOCKER_PASSWORD")
)

// GVRs for the resources to be monitored
var deploymentGVR, daemonsetGVR = schema.GroupVersionResource{
	Version:  "v1",
	Group:    "apps",
	Resource: "deployments",
}, schema.GroupVersionResource{
	Version:  "v1",
	Group:    "apps",
	Resource: "daemonsets",
}

var logger *zap.Logger

func log() *zap.Logger {
	setupLogger()
	return logger
}

func setupLogger() {
	var once sync.Once
	once.Do(func() {
		logger, _ = zap.NewProduction()
	})
}

type Controller struct {
	dynamicClientSet dynamic.Interface

	// workqueue for deployments
	queue workqueue.RateLimitingInterface

	factory dynamicinformer.DynamicSharedInformerFactory
	// recorder to record events on the resources
	recorder record.EventRecorder
}

func NewController(kc kubernetes.Interface, dc dynamic.Interface) *Controller {

	// Recorder init
	log().Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kc.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "retag-image-controller"})

	// Controller init
	log().Info("Creating controller")
	controller := &Controller{
		dynamicClientSet: dc,
		queue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "deployments"),
		recorder:         recorder,
		factory:          dynamicinformer.NewFilteredDynamicSharedInformerFactory(dc, 0, v1.NamespaceAll, nil),
	}

	return controller
}

func (c *Controller) Watch(stopCh <-chan struct{}) {
	handlers := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			uObj, ok := obj.(*unstructured.Unstructured)
			if !ok {
				log().Error("unable to type assert object into *unstructured.Unstructured")
			}

			if uObj.GetNamespace() != "kube-system" {
				c.queue.Add(obj)
			}

		},
		UpdateFunc: func(oldObj, obj interface{}) {
			uObj, ok := obj.(*unstructured.Unstructured)
			if !ok {
				log().Error("unable to type assert object into *unstructured.Unstructured")
			}
			if uObj.GetNamespace() != "kube-system" {
				c.queue.Add(obj)
			}
		},
		DeleteFunc: func(obj interface{}) {},
	}

	c.factory.ForResource(deploymentGVR).Informer().AddEventHandler(handlers)
	c.factory.ForResource(daemonsetGVR).Informer().AddEventHandler(handlers)

	c.factory.Start(stopCh)
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	log().Info("Starting retag-image-controller controller")

	// Wait for the caches to be synced before starting workers
	log().Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	log().Info("Startingworkers")
	// Launch two workers to process objects
	for i := 0; i < threadiness; i++ {
		go func() {
			wait.Until(c.runWorker, time.Second, stopCh)
		}()
	}

	log().Info("Started workers")
	<-stopCh
	log().Info("Shutting down workers")

	return nil
}

func (c *Controller) runWorker() {
	for c.processObject() {
	}
}

func (c *Controller) processObject() bool {
	// Wait until there is a new item in the working queue
	resource, quit := c.queue.Get()
	if quit {
		return false
	}

	defer c.queue.Done(resource)

	// Invoke the method containing the business logic
	c.bussinessLogic(resource)
	return true
}

func (c *Controller) bussinessLogic(obj interface{}) {

	uObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		log().Error("unable to type assert object into *unstructured.Unstructured")
	}

	log().Info("Patching Object starts now")
	patch, err := getPatchforObject(uObj, log().With(
		zap.String("name", uObj.GetName()),
		zap.String("namespace", uObj.GetNamespace()),
		zap.String("kind", uObj.GetKind())))
	if err != nil {
		if !strings.Contains(err.Error(), errAlreadyReplaced.Error()) {
			log().Error("unable to create patch for object", zap.Any("patch", patch), zap.String("error", err.Error()))
			c.recorder.Event(uObj.DeepCopyObject(), corev1.EventTypeNormal, "IMAGE_CHANGE_OPERATION_FAILED", fmt.Sprintf("image replace operation failed, due to error: %s", err.Error()))
		}
		return
	}

	if _, err = c.dynamicClientSet.Resource(schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: strings.ToLower(uObj.GroupVersionKind().Kind) + "s",
	}).Namespace(uObj.GetNamespace()).Patch(context.TODO(), uObj.GetName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		log().Error("unable to patch resource", zap.Error(err))
		c.recorder.Event(uObj.DeepCopyObject(), corev1.EventTypeWarning, "PATCH_OPERATION_FAILED", "Image change operation failed")
		return
	}

	uObj.DeepCopyObject()
	c.recorder.Event(uObj.DeepCopyObject(), corev1.EventTypeNormal, "IMAGE_CHANGE_OPERATION_PASSED", "Image has been backedup and replaced")
}

func getPatchforObject(uObj *unstructured.Unstructured, logger *zap.Logger) ([]byte, error) {

	switch uObj.GetKind() {

	case "Deployment":
		deploymentBytes, err := json.Marshal(uObj.Object)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal 'pod template spec'")
		}

		var deployment appsv1.Deployment
		if err := json.Unmarshal(deploymentBytes, &deployment); err != nil {
			return nil, fmt.Errorf("unable to unmarshal 'pod template spec' into corev1.PodTemplateSpec")
		}

		modifiedDeployment := deployment.DeepCopy()
		if err := fixImagesInPodSpec(&modifiedDeployment.Spec.Template, logger); err != nil {
			return nil, fmt.Errorf("unable to fix image for deployment with name: %s, and namespace: %s, error: %w", deployment.Name, deployment.Namespace, err)
		}

		modifiedDeploymentBytes, err := json.Marshal(modifiedDeployment)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal 'pod template spec'")
		}

		patchBytes, err := strategicpatch.CreateTwoWayMergePatch(deploymentBytes, modifiedDeploymentBytes, appsv1.Deployment{})
		if err != nil {
			return nil, fmt.Errorf("unable to generate patch bytes, error: %w", err)
		}

		return patchBytes, nil

	case "DaemonSet":
		daemonsetBytes, err := json.Marshal(uObj.Object)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal 'pod template spec'")
		}

		var daemonset appsv1.DaemonSet
		if err := json.Unmarshal(daemonsetBytes, &daemonset); err != nil {
			return nil, fmt.Errorf("unable to unmarshal 'pod template spec' into corev1.PodTemplateSpec")
		}

		modifiedDaemonset := daemonset.DeepCopy()
		fixImagesInPodSpec(&modifiedDaemonset.Spec.Template, logger)

		modifiedDaemonsetBytes, err := json.Marshal(modifiedDaemonset)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal 'pod template spec'")
		}

		patchBytes, err := strategicpatch.CreateTwoWayMergePatch(daemonsetBytes, modifiedDaemonsetBytes, appsv1.DaemonSet{})
		if err != nil {
			return nil, fmt.Errorf("unable to generate patch bytes, error: %w", err)
		}

		return patchBytes, nil

	default:
		return nil, fmt.Errorf("unknown resource found in workqueue with kind: %s", uObj.GetKind())
	}
}

func fixImagesInPodSpec(podSpec *corev1.PodTemplateSpec, logger *zap.Logger) error {
	if err := imageManipulationsContainers(podSpec.Spec.Containers, logger); err != nil {
		return fmt.Errorf("unable to manipulate the image in container, error: %w", err)
	}

	// as these fields are `omitempty`, their can be cases in which they return nil
	// i.e no key found in json.
	// to handle that, just don't return error from here, as we want to go further.
	if err := imageManipulationsContainers(podSpec.Spec.InitContainers, logger); err != nil {
		return fmt.Errorf("unable to manipulate the image in container, error: %w", err)
	}

	if err := imageManipulationsEphemeralContainers(podSpec.Spec.EphemeralContainers, logger); err != nil {
		return fmt.Errorf("unable to manipulate the image in container, error: %w", err)
	}

	return nil
}

func imageManipulationsContainers(containers []corev1.Container, logger *zap.Logger) error {
	// fetch each image out of the container by iterating over it.
	for i, container := range containers {
		retaggedImage, err := retagImageWithPush(container.Image)
		if err != nil {
			return fmt.Errorf("unable to create/push image, error: %w", err)
		}
		containers[i].Image = retaggedImage
	}
	return nil
}

func imageManipulationsEphemeralContainers(containers []corev1.EphemeralContainer, logger *zap.Logger) error {
	// fetch each image out of the container by iterating over it.
	for i, container := range containers {
		retaggedImage, err := retagImageWithPush(container.Image)
		if err != nil {
			return fmt.Errorf("unable to create/push image, error: %w", err)
		}
		containers[i].Image = retaggedImage
	}
	return nil
}

func retagImageWithPush(image string) (string, error) {
	slashImage := strings.Split(image, "/")

	if err := checkIfImageAlreadyReplaced(slashImage); err != nil {
		return "", err
	}

	newImage := username + "/" + strings.Split(image, "/")[len(slashImage)-1]

	ref, err := name.ParseReference(image)
	if err != nil {
		return "", fmt.Errorf("unable to parse reference for image: %s, error: %w", image, err)
	}

	img, err := remote.Image(ref)
	if err != nil {
		return "", fmt.Errorf("unable to pull image for image name %s, error: %w", image, err)
	}

	authObj := authn.FromConfig(authn.AuthConfig{
		Username: username,
		Password: password,
	})

	ownRef, err := name.ParseReference(newImage)
	if err != nil {
		return "", fmt.Errorf("unable to parse reference for image: %s, error: %w", newImage, err)
	}

	if err := remote.Write(ownRef, img, remote.WithAuth(authObj)); err != nil {
		return "", fmt.Errorf("unable to push image for image name %s, error: %w", image, err)
	}

	log().Info("ref changed for image", zap.Any("oldRef", ref), zap.Any("newRef", ownRef))

	return newImage, nil
}

// checkIfImageAlreadyReplaced returns error if the image is already replaced
// no need to process that object
func checkIfImageAlreadyReplaced(slashImage []string) error {
	if len(slashImage) < 2 {
		return nil
	}

	if slashImage[len(slashImage)-2] == username {
		return errAlreadyReplaced
	}

	return nil
}
