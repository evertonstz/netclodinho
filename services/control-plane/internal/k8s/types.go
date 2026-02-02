package k8s

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Sandbox CRD GVR
var SandboxGVR = schema.GroupVersionResource{
	Group:    "agents.x-k8s.io",
	Version:  "v1alpha1",
	Resource: "sandboxes",
}

// SandboxGVK is the GroupVersionKind for Sandbox
var SandboxGVK = schema.GroupVersionKind{
	Group:   "agents.x-k8s.io",
	Version: "v1alpha1",
	Kind:    "Sandbox",
}

// Sandbox represents the Sandbox CRD
type Sandbox struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SandboxSpec   `json:"spec,omitempty"`
	Status            SandboxStatus `json:"status,omitempty"`
}

// SandboxSpec defines the desired state
type SandboxSpec struct {
	PodTemplate          PodTemplateSpec `json:"podTemplate,omitempty"`
	VolumeClaimTemplates []PVCTemplate   `json:"volumeClaimTemplates,omitempty"`
}

// PodTemplateSpec is a simplified pod template
type PodTemplateSpec struct {
	Metadata PodMetadata `json:"metadata,omitempty"`
	Spec     PodSpec     `json:"spec,omitempty"`
}

// PodMetadata is pod metadata with annotations support for Kata VM sizing
type PodMetadata struct {
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// PodSpec is a simplified pod spec
type PodSpec struct {
	RuntimeClassName string      `json:"runtimeClassName,omitempty"`
	Containers       []Container `json:"containers,omitempty"`
	Volumes          []Volume    `json:"volumes,omitempty"`
}

// Volume defines a pod volume
type Volume struct {
	Name                  string                             `json:"name"`
	PersistentVolumeClaim *PersistentVolumeClaimVolumeSource `json:"persistentVolumeClaim,omitempty"`
}

// PersistentVolumeClaimVolumeSource references an existing PVC
type PersistentVolumeClaimVolumeSource struct {
	ClaimName string `json:"claimName"`
}

// Container is a simplified container spec
type Container struct {
	Name            string              `json:"name"`
	Image           string              `json:"image"`
	Ports           []Port              `json:"ports,omitempty"`
	Env             []EnvVar            `json:"env,omitempty"`
	EnvFrom         []EnvFromSource     `json:"envFrom,omitempty"`
	VolumeMounts    []VolumeMount       `json:"volumeMounts,omitempty"`
	SecurityContext *SecurityContext    `json:"securityContext,omitempty"`
	ReadinessProbe  *Probe              `json:"readinessProbe,omitempty"`
	Resources       *ContainerResources `json:"resources,omitempty"`
}

// ContainerResources defines resource requests for K8s scheduling.
// We only use requests (not limits) to avoid cgroup throttling with Kata.
type ContainerResources struct {
	Requests map[string]string `json:"requests,omitempty"`
}

// SandboxResourceConfig holds resource configuration for a sandbox VM.
// These map to Kata annotations for VM sizing and K8s requests for scheduling.
type SandboxResourceConfig struct {
	VCPUs    int32 // Number of vCPUs for the VM
	MemoryMB int32 // Memory in MiB for the VM
}

// Port defines a container port
type Port struct {
	ContainerPort int    `json:"containerPort"`
	Name          string `json:"name,omitempty"`
}

// EnvVar defines an environment variable
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// EnvFromSource defines a source for environment variables
type EnvFromSource struct {
	SecretRef *SecretRef `json:"secretRef,omitempty"`
}

// SecretRef references a secret
type SecretRef struct {
	Name string `json:"name"`
}

// VolumeMount defines a volume mount
type VolumeMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}

// SecurityContext defines security options
type SecurityContext struct {
	Privileged             bool `json:"privileged,omitempty"`
	ReadOnlyRootFilesystem bool `json:"readOnlyRootFilesystem,omitempty"`
}

// Probe defines a health probe
type Probe struct {
	HTTPGet             *HTTPGetAction `json:"httpGet,omitempty"`
	Exec                *ExecAction    `json:"exec,omitempty"`
	InitialDelaySeconds int            `json:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int            `json:"periodSeconds,omitempty"`
}

// HTTPGetAction defines an HTTP GET probe
type HTTPGetAction struct {
	Path string `json:"path"`
	Port int    `json:"port"`
}

// ExecAction defines an exec probe
type ExecAction struct {
	Command []string `json:"command"`
}

// PVCTemplate defines a PVC template
type PVCTemplate struct {
	Metadata metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec     PVCSpec           `json:"spec,omitempty"`
}

// PVCSpec defines PVC spec
type PVCSpec struct {
	AccessModes      []string             `json:"accessModes,omitempty"`
	StorageClassName string               `json:"storageClassName,omitempty"`
	Resources        ResourceRequirements `json:"resources,omitempty"`
	DataSource       *DataSource          `json:"dataSource,omitempty"`
}

// DataSource for restoring PVC from VolumeSnapshot
type DataSource struct {
	APIGroup string `json:"apiGroup,omitempty"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
}

// ResourceRequirements defines resource requests
type ResourceRequirements struct {
	Requests map[string]string `json:"requests,omitempty"`
}

// SandboxStatus defines the observed state
type SandboxStatus struct {
	ServiceFQDN string             `json:"serviceFQDN,omitempty"`
	Conditions  []SandboxCondition `json:"conditions,omitempty"`
}

// SandboxCondition defines a condition
type SandboxCondition struct {
	Type               string      `json:"type"`
	Status             string      `json:"status"`
	Reason             string      `json:"reason,omitempty"`
	Message            string      `json:"message,omitempty"`
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// SandboxList is a list of Sandbox resources
type SandboxList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Sandbox `json:"items"`
}

// DeepCopyObject implements runtime.Object
func (s *Sandbox) DeepCopyObject() runtime.Object {
	return s.DeepCopy()
}

// DeepCopy creates a deep copy
func (s *Sandbox) DeepCopy() *Sandbox {
	if s == nil {
		return nil
	}
	out := new(Sandbox)
	*out = *s
	out.ObjectMeta = *s.ObjectMeta.DeepCopy()
	return out
}

// DeepCopyObject implements runtime.Object
func (s *SandboxList) DeepCopyObject() runtime.Object {
	return s.DeepCopy()
}

// DeepCopy creates a deep copy
func (s *SandboxList) DeepCopy() *SandboxList {
	if s == nil {
		return nil
	}
	out := new(SandboxList)
	*out = *s
	out.ListMeta = *s.ListMeta.DeepCopy()
	if s.Items != nil {
		out.Items = make([]Sandbox, len(s.Items))
		for i := range s.Items {
			out.Items[i] = *s.Items[i].DeepCopy()
		}
	}
	return out
}

// GetObjectKind implements runtime.Object
func (s *Sandbox) GetObjectKind() schema.ObjectKind {
	return &s.TypeMeta
}

// GetObjectKind implements runtime.Object
func (s *SandboxList) GetObjectKind() schema.ObjectKind {
	return &s.TypeMeta
}

// SandboxStatusInfo is returned by GetStatus
type SandboxStatusInfo struct {
	Exists      bool
	Ready       bool
	ServiceFQDN string
	Error       string
}

// SandboxInfo contains basic information about a sandbox
type SandboxInfo struct {
	SessionID   string
	ServiceFQDN string
	Ready       bool
}

// IsReady returns true if the sandbox is ready
func (s *Sandbox) IsReady() bool {
	for _, c := range s.Status.Conditions {
		if c.Type == "Ready" {
			return c.Status == "True"
		}
	}
	return false
}

// GetError returns the error message only for actual failures, not transient states
func (s *Sandbox) GetError() string {
	for _, c := range s.Status.Conditions {
		if c.Type == "Ready" && c.Status == "False" {
			// Don't treat pending/creating states as errors
			if c.Reason == "DependenciesPending" || c.Reason == "PodPending" || c.Reason == "Creating" {
				return ""
			}
			// Check if message indicates a transient state (pod still starting up)
			if strings.Contains(c.Message, "phase: Pending") ||
				strings.Contains(c.Message, "phase: ContainerCreating") ||
				strings.Contains(c.Message, "Running but not Ready") {
				return ""
			}
			return c.Message
		}
	}
	return ""
}

// GetOriginalPodName returns the original pod name from the sandbox annotation.
// This is used for warm pool mode where the sandbox is renamed when claimed,
// but we need the original pod name to look up the warm agent connection.
func (s *Sandbox) GetOriginalPodName() string {
	if s.Annotations != nil {
		return s.Annotations["agents.x-k8s.io/pod-name"]
	}
	return ""
}

// SandboxClaim CRD GVR (extensions API group)
var SandboxClaimGVR = schema.GroupVersionResource{
	Group:    "extensions.agents.x-k8s.io",
	Version:  "v1alpha1",
	Resource: "sandboxclaims",
}

// SandboxClaim represents a claim for a sandbox from the warm pool
type SandboxClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SandboxClaimSpec   `json:"spec,omitempty"`
	Status            SandboxClaimStatus `json:"status,omitempty"`
}

// SandboxClaimSpec defines the desired state of a SandboxClaim
type SandboxClaimSpec struct {
	SandboxTemplateRef SandboxTemplateRef `json:"sandboxTemplateRef"`
}

// SandboxTemplateRef references a SandboxTemplate
type SandboxTemplateRef struct {
	Name string `json:"name"`
}

// SandboxClaimStatus defines the observed state of a SandboxClaim
type SandboxClaimStatus struct {
	Conditions []SandboxCondition `json:"conditions,omitempty"`
	Sandbox    *SandboxReference  `json:"sandbox,omitempty"`
}

// SandboxReference references an assigned Sandbox
// Note: CRD uses capital "Name" field
type SandboxReference struct {
	Name string `json:"Name"`
}

// SandboxClaimInfo contains basic information about a claim
type SandboxClaimInfo struct {
	SessionID   string
	Bound       bool
	SandboxName string
}

// IsBound returns true if the claim has a sandbox assigned
func (c *SandboxClaim) IsBound() bool {
	return c.Status.Sandbox != nil && c.Status.Sandbox.Name != ""
}

// GetBoundSandboxName returns the name of the bound sandbox, or empty string
func (c *SandboxClaim) GetBoundSandboxName() string {
	if c.Status.Sandbox == nil {
		return ""
	}
	return c.Status.Sandbox.Name
}

// GetError returns the error message from claim conditions
func (c *SandboxClaim) GetError() string {
	for _, cond := range c.Status.Conditions {
		if cond.Type == "Ready" && cond.Status == "False" {
			return cond.Message
		}
	}
	return ""
}

// ============================================================================
// VolumeSnapshot types (for snapshot/restore functionality)
// ============================================================================

// VolumeSnapshotGVR is the GroupVersionResource for VolumeSnapshot
var VolumeSnapshotGVR = schema.GroupVersionResource{
	Group:    "snapshot.storage.k8s.io",
	Version:  "v1",
	Resource: "volumesnapshots",
}

// VolumeSnapshotClassGVR is the GroupVersionResource for VolumeSnapshotClass
var VolumeSnapshotClassGVR = schema.GroupVersionResource{
	Group:    "snapshot.storage.k8s.io",
	Version:  "v1",
	Resource: "volumesnapshotclasses",
}

// VolumeSnapshot represents a K8s VolumeSnapshot
type VolumeSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              VolumeSnapshotSpec    `json:"spec,omitempty"`
	Status            *VolumeSnapshotStatus `json:"status,omitempty"`
}

// VolumeSnapshotSpec defines the desired state of a VolumeSnapshot
type VolumeSnapshotSpec struct {
	VolumeSnapshotClassName *string              `json:"volumeSnapshotClassName,omitempty"`
	Source                  VolumeSnapshotSource `json:"source"`
}

// VolumeSnapshotSource specifies the source for a snapshot
type VolumeSnapshotSource struct {
	PersistentVolumeClaimName *string `json:"persistentVolumeClaimName,omitempty"`
	VolumeSnapshotContentName *string `json:"volumeSnapshotContentName,omitempty"`
}

// VolumeSnapshotStatus defines the observed state of a VolumeSnapshot
type VolumeSnapshotStatus struct {
	BoundVolumeSnapshotContentName *string              `json:"boundVolumeSnapshotContentName,omitempty"`
	ReadyToUse                     *bool                `json:"readyToUse,omitempty"`
	CreationTime                   *metav1.Time         `json:"creationTime,omitempty"`
	RestoreSize                    *string              `json:"restoreSize,omitempty"`
	Error                          *VolumeSnapshotError `json:"error,omitempty"`
}

// VolumeSnapshotError describes an error encountered during snapshot operation
type VolumeSnapshotError struct {
	Time    *metav1.Time `json:"time,omitempty"`
	Message *string      `json:"message,omitempty"`
}

// IsReady returns true if the snapshot is ready to use
func (vs *VolumeSnapshot) IsReady() bool {
	return vs.Status != nil && vs.Status.ReadyToUse != nil && *vs.Status.ReadyToUse
}

// GetError returns the error message if any
func (vs *VolumeSnapshot) GetError() string {
	if vs.Status != nil && vs.Status.Error != nil && vs.Status.Error.Message != nil {
		return *vs.Status.Error.Message
	}
	return ""
}

// VolumeSnapshotInfo contains basic information about a snapshot
type VolumeSnapshotInfo struct {
	Name         string
	SessionID    string
	SnapshotID   string
	Ready        bool
	CreationTime *metav1.Time
	Error        string
}
