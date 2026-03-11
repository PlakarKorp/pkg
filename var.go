package pkg

import "slices"

type ResourceClass string

type ResourceSubClass string

type ConnectorType string

const (
	ResourceClassUndefined     ResourceClass = ""
	ResourceClassUnknown       ResourceClass = "unknown"
	ResourceClassAnalytics     ResourceClass = "analytics"
	ResourceClassAPI           ResourceClass = "api"
	ResourceClassBlockStorage  ResourceClass = "block-storage"
	ResourceClassCompute       ResourceClass = "compute"
	ResourceClassDatabase      ResourceClass = "database"
	ResourceClassFileStorage   ResourceClass = "file-storage"
	ResourceClassIdentity      ResourceClass = "identity"
	ResourceClassMessaging     ResourceClass = "messaging"
	ResourceClassNetwork       ResourceClass = "network"
	ResourceClassObjectStorage ResourceClass = "object-storage"
	ResourceClassObservability ResourceClass = "observability"
	ResourceClassRegistry      ResourceClass = "registry"
	ResourceClassSecurity      ResourceClass = "security"

	ResourceSubClassUndefined ResourceSubClass = ""
	ResourceSubClassUnknown   ResourceSubClass = "unknown"
	ResourceSubClassGCS       ResourceSubClass = "gcs"
	ResourceSubClassS3        ResourceSubClass = "s3"

	ConnectorTypeImporter       ConnectorType = "importer"
	ConnectorTypeExporter       ConnectorType = "exporter"
	ConnectorTypeStorage        ConnectorType = "storage"
	ConnectorTypeSecretProvider ConnectorType = "secret_provider"
	ConnectorTypeInventory      ConnectorType = "inventory"
)

var resourceClasses []ResourceClass = []ResourceClass{
	ResourceClassUndefined,
	ResourceClassUnknown,
	ResourceClassAnalytics,
	ResourceClassAPI,
	ResourceClassBlockStorage,
	ResourceClassCompute,
	ResourceClassDatabase,
	ResourceClassFileStorage,
	ResourceClassIdentity,
	ResourceClassMessaging,
	ResourceClassNetwork,
	ResourceClassObjectStorage,
	ResourceClassObservability,
	ResourceClassRegistry,
	ResourceClassSecurity,
}

var resourceSubClasses []ResourceSubClass = []ResourceSubClass{
	ResourceSubClassUndefined,
	ResourceSubClassUnknown,
	ResourceSubClassS3,
	ResourceSubClassGCS,
}

var connectorTypes []ConnectorType = []ConnectorType{
	ConnectorTypeExporter,
	ConnectorTypeImporter,
	ConnectorTypeStorage,
	ConnectorTypeSecretProvider,
	ConnectorTypeInventory,
}

var resourceClassTree map[ResourceClass][]ResourceSubClass = map[ResourceClass][]ResourceSubClass{
	ResourceClassObjectStorage: {ResourceSubClassGCS, ResourceSubClassS3},
}

func (c ResourceClass) IsValid() bool {
	return slices.Contains(resourceClasses, c)
}

func (c ResourceSubClass) IsValid() bool {
	return slices.Contains(resourceSubClasses, c)
}

func (c ResourceSubClass) IsSubClassOf(parent ResourceClass) bool {
	subclasses, found := resourceClassTree[parent]
	if !found {
		return false
	}
	return slices.Contains(subclasses, c)
}

func (c ConnectorType) IsValid() bool {
	return slices.Contains(connectorTypes, c)
}
