package stackpointio

import "time"

// Organization is the top level of the hierarchy
type Organization struct {
	Name       string `json:"name"`
	PrimaryKey int    `json:"pk"`
}

// User is a stackpoint user
type User struct {
	Username  string `json:"username"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// UserKey is a token of some type
type UserKey struct {
	PrimaryKey int    `json:"pk"`
	Type       string `json:"key_type"`
	KeysetID   string `json:"keyset_id"`
}

// UserKeyset groups keys together in a category
type UserKeyset struct {
	Name       string    `json:"name"`
	PrimaryKey int       `json:"pk"`
	Category   string    `json:"category"`
	Entity     string    `json:"entity"`
	Keys       []UserKey `json:"keys"`
}

// UserProfile includes detailed information about a StackPointCloud user
type UserProfile struct {
	User
	PrimaryKey     int            `json:"pk"`
	OrgMemberships []Organization `json:"org_memberships"`
	Keysets        []UserKeyset   `json:"keysets"`
}

// Solution is a application or process running with or on a kubernetes cluster,
// including "deis", "tectonic", "prometheus" and others
type Solution struct {
	PrimaryKey int       `json:"pk"`
	Solution   string    `json:"solution"`
	URL        string    `json:"url"`
	Username   string    `json:"username,omitempty"`
	Password   string    `json:"password,omitempty"`
	GitRepo    string    `json:"git_repo,omitempty"`
	GitPath    string    `json:"git_path,omitempty"`
	Created    time.Time `json:"created"`
	Updated    time.Time `json:"updated,omitempty"`
}

// Cluster describes a Kubernetes cluster in the StackPointCloud system
type Cluster struct {
	PrimaryKey         int        `json:"pk"`
	Name               string     `json:"name"`
	OrganizationKey    int        `json:"org"`
	InstanceID         string     `json:"instance_id"`
	Provider           string     `json:"provider"`
	ProviderKey        int        `json:"provider_keyset"`
	ProviderKeyName    string     `json:"provider_keyset_name"`
	Region             string     `json:"region"`
	Zone               string     `json:"zone,omitempty"`
	State              string     `json:"state"`
	ProjectID          string     `json:"project_id,omitempty"`
	Owner              int        `json:"owner"`
	Notified           bool       `json:"notified,omitempty"`
	KubernetesVersion  string     `json:"k8s_version"`
	Created            time.Time  `json:"created"`
	Updated            time.Time  `json:"updated,omitempty"`
	DashboardEnabled   bool       `json:"k8s_dashboard_enabled"`
	DashboardInstalled bool       `json:"k8s_dashboard_installed"`
	KubeonfigPath      string     `json:"kubeconfig_path"`
	NodeCount          int        `json:"node_count"`
	Platform           string     `json:"platform"`
	Image              string     `json:"image"`
	Channel            string     `json:"channel"`
	Solutions          []Solution `json:"solutions"`
}

// Node describes a node in a cluster.  The string field Size is provider-specific
type Node struct {
	PrimaryKey int       `json:"pk"`
	Name       string    `json:"name"`
	ClusterID  int       `json:"cluster"`
	InstanceID string    `json:"instance_id"`
	Role       string    `json:"role"`
	PrivateIP  string    `json:"private_ip"`
	PublicIP   string    `json:"public_ip"`
	Platform   string    `json:"platform"`
	Image      string    `json:"image"`
	Location   string    `json:"location"`
	Size       string    `json:"size"`
	State      string    `json:"state"` // draft, building, provisioned, running, deleting, deleted
	Created    time.Time `json:"created"`
	Updated    time.Time `json:"updated,omitempty"`
}

// NodeAdd encapsulates the details of a call to add nodes to a cluster.
type NodeAdd struct {
	Size  string `json:"size"`
	Count int    `json:"node_count"`
}

// PersistentVolume is the representation of a Kubernetes PersistentVolume in
// StackPointCloud, and includes details of the PersistentVolumeClaim
type PersistentVolume struct {
	PrimaryKey      int       `json:"pk"`
	Name            string    `json:"name"`
	ClusterID       int       `json:"cluster"`
	VolumeID        string    `json:"volume_id"`
	VolumeType      string    `json:"volume_type"`
	Claim           string    `json:"claim_name"`
	NameSpace       string    `json:"namespace"`
	SizeGB          int       `json:"size"`
	AccessMode      string    `json:"access_mode"`
	State           string    `json:"state"`
	RecyclingPolicy string    `json:"recycling_policy"`
	Owner           int       `json:"owner"`
	Deleted         bool      `json:"deleted,omitempty"`
	Backend         string    `json:"backend"`
	Created         time.Time `json:"created"`
}

// BuildLogEntry is an event log for the cluster build process
type BuildLogEntry struct {
	ClusterID     int       `json:"cluster"`
	EventCategory string    `json:"event_category"`
	EventType     string    `json:"event_type"`
	EventState    string    `json:"event_state"`
	Message       string    `json:"message"`
	Details       string    `json:"details"`
	Reference     string    `json:"reference"`
	Created       time.Time `json:"created"`
}
