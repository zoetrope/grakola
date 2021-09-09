package controllers

import "k8s.io/apimachinery/pkg/runtime/schema"

var (
	// certManagerGroupVersion is cert-manager group version which is used to uniquely identifies the API
	certManagerGroupVersion = schema.GroupVersion{Group: "cert-manager.io", Version: "v1"}
)
