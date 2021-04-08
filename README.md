# RETAG-IMAGE CONTROLLER

### Description
- This is Greenfield Project with a barebone kubernetes custom controller which retag the existing images available in cluster.
- Pushes it to a backup repository (provided as ENV variable).
- And changes the images in Deployments and Daemonsets using them.


###  Technical Description
- Creation of Kubernetes and Dynamic Client in `main` func
- Passed on to create a SharedDynamicInformer with Informers on `deployments`, and `daemonsets`
- Implemented a simple workqueue(/queue) to push the object's meta data in case of `Create` and `Update` events.
- All the elements in this queue are processed by a function called `Run`, which inturn runs a number of goroutines functions called `runWorker`, which uses a wrapper over goroutines to create multiple `workers` and manages them (i.e restarts).
- The main bussiness logic is consistuted in a function called `bussinessLogic`, which tries to get a common resource i.e `corev1.PodSpecTemplate` (i.e common in both `appsv1.Deployment, and appsv1.DaemonSet`), and then processes the containers inside them.
- All images in container list (i.e `containers`, `initContainers` and `ephermeralContainers`), are re-tagged in a backup repository, and then pushed onto DockerHub, and replaced in the resource itself.
- Then we find the `json patches` to perform `PATCH` operation rather than `UPDATE`, which will atleast the reduce few `Update` events to be catched.
- Added comments on most the functions to provide context.


### Few features of this controller to be highlighted
- We just pass the resource's meta data in the work queue, so we can get the latest last updated resource, and don't perform any business logic (i.e pushing the image, and `PATCH` operation)
- Every CRUD operations uses Dynamic Client, which can help us perform these opertions in a single function call rather than different for both the resources.
- The `controller.resource` struct uses resourceType as the `unstructured.Unstructured` object's kind, as with this we won't get both deployment and daemonset with the same name and namespace. For eg: if we have a `dummy` resource named deployment and daemonset in `default` namespace, thier can be a case in which we process both of these objects rather than one as we might need to `GET` both deployment and daemonset.
- Signal handling has been added to provide graceful exit.
- Singleton logging object in `controller` package, with name, namespace and kind of processing object to be printed with the error, or info.
- Used multiple goroutines as workers to increase concurrency.
- Added `recorder` to push events on the processed object.


### Things which can still be improved
- Context propagation for k8s calls, to provide cancellations.
- Handling the the check for already replaced image before pushing them into workqueue.
- Taking the DOCKER_USERNAME and DOCKER_PASSWORD in k8s secrets to provide atleast base64 encoded data.


### How to deploy:
- Clone this repository, and switch to the latest release branch (i.e `2.0.0`)
- Please ensure you are at the root of the project.
- Replace the backup docker repository in `./deploy/deployment.yaml` ENV variables before applying.
- Execure this command to deploy the controller deployment, and RBAC related k8s resources in deploy folder. `kubectl apply -f ./deploy`
- One more thing, just ensure you are using an image that doesn't have `-dev` prefix on image. These images are for development, and not for production use.
- 