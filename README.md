# RETAG-IMAGE CONTROLLER

### Description
- This is Greenfield Project with a barebone kubernetes custom controller which retag the existing images available in cluster.
- Pushes it to a backup repository (provided as ENV variable).
- And changes the images in Deployments and Daemonsets using them.


###  Technical Description
- Creation of Kubernetes and Dynamic Client in `main` func
- Passed on to create a SharedDynamicInformer with Informers on `deployments`, and `daemonsets`
- Implemented a simple workqueue(/queue) to push the objects in case of `Create` and `Update` events.
- All the elements in this queue are processed by a function called `Run`, which inturn runs a number of goroutines functions called `runWorker`, which uses a wrapper over goroutines to create multiple `workers` and manages them (i.e restarts).
- The main bussiness logic is consistuted in a function called `bussinessLogic`, which tries to get a common resource i.e `corev1.PodSpecTemplate` (i.e common in both `appsv1.Deployment, and appsv1.DaemonSet`), and then processes the containers inside them.
- All images in container list (i.e `containers`, `initContainers` and `ephermeralContainers`), are re-tagged in a backup repository, and then pushed onto DockerHub, and replaced in the resource itself.
- Then we find the `json patches` to perform `PATCH` operation rather than `UPDATE`, which will atleast the reduce few `Update` events to be catched.
