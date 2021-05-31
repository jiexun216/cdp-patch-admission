
# Kubernetes Admission Webhook cdp-patch-admission

This tutoral shows how to build and deploy an [AdmissionWebhook](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#admission-webhooks). which designed to modify StatefulSet or Deployment or Job securityContext.

The mutating webhook in the cdp-patch-admission adds a specific annotation with `mutated` set as the value.

## Prerequisites

Kubernetes 1.9.0 or above with the `admissionregistration.k8s.io/v1beta1` API enabled. Verify that by the following command:
```
kubectl api-versions | grep admissionregistration.k8s.io/v1beta1
```
The result should be:
```
admissionregistration.k8s.io/v1beta1
```

In addition, the `MutatingAdmissionWebhook` and `ValidatingAdmissionWebhook` admission controllers should be added and listed in the correct order in the admission-control flag of kube-apiserver.

## Quickstart

You can build docker image using the included make chart. run this in the root directory.

    $ make docker-build

You can push docker image using the included make chart. run this in the root directory.

    $ make docker-push

You can deploy the webhook server using the included make chart. run this in the root directory.

    $ make deploy

You can undeploy the webhook server using the included make chart. run this in the root directory.

    $ make undeploy
    
    
At last, test the Admission webhooks server receive admission requests and modify object.
