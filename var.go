package pkg

import "slices"

type ResourceClass string

type ResourceSubClass string

type ConnectorType string

const (
	ResourceClassUndefined     ResourceClass = ""
	ResourceClassUnknown       ResourceClass = "unknown"
	ResourceClassCompute       ResourceClass = "compute"
	ResourceClassDatabase      ResourceClass = "database"
	ResourceClassBlockStorage  ResourceClass = "block-storage"
	ResourceClassObjectStorage ResourceClass = "object-storage"
	ResourceClassFileStorage   ResourceClass = "file-storage"
	ResourceClassNetwork       ResourceClass = "network"
	ResourceClassIdentity      ResourceClass = "identity"
	ResourceClassSecurity      ResourceClass = "security"
	ResourceClassMessaging     ResourceClass = "messaging"
	ResourceClassAPI           ResourceClass = "api"
	ResourceClassObservability ResourceClass = "observability"
	ResourceClassAnalytics     ResourceClass = "analytics"

	ResourceSubClassUndefined ResourceSubClass = ""
	ResourceSubClassUnknown   ResourceSubClass = "unknown"
	ResourceSubClassS3        ResourceSubClass = "s3"
	ResourceSubClassGCS       ResourceSubClass = "gcs"

	ConnectorTypeImporter       ConnectorType = "importer"
	ConnectorTypeExporter       ConnectorType = "exporter"
	ConnectorTypeStorage        ConnectorType = "storage"
	ConnectorTypeSecretProvider ConnectorType = "secret_provider"
	ConnectorTypeInventory      ConnectorType = "inventory"
)

var resourceClasses []ResourceClass = []ResourceClass{
	ResourceClassUndefined,
	ResourceClassUnknown,
	ResourceClassCompute,
	ResourceClassDatabase,
	ResourceClassBlockStorage,
	ResourceClassObjectStorage,
	ResourceClassAPI,
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

func (c ResourceClass) IsValid() bool {
	return slices.Contains(resourceClasses, c)
}

func (c ResourceSubClass) IsValid() bool {
	return slices.Contains(resourceSubClasses, c)
}

func (c ConnectorType) IsValid() bool {
	return slices.Contains(connectorTypes, c)
}
