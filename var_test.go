package pkg

import "testing"

func TestResourceClassIsValid(t *testing.T) {
	valid := []ResourceClass{
		ResourceClassUndefined,
		ResourceClassUnknown,
		ResourceClassAnalytics,
		ResourceClassBlockStorage,
		ResourceClassCompute,
		ResourceClassDatabase,
		ResourceClassFileStorage,
		ResourceClassHypervisor,
		ResourceClassIdentity,
		ResourceClassMessaging,
		ResourceClassNetwork,
		ResourceClassObjectStorage,
		ResourceClassObservability,
		ResourceClassRegistry,
		ResourceClassSecurity,
		ResourceClassService,
	}
	for _, c := range valid {
		if !c.IsValid() {
			t.Errorf("ResourceClass(%q).IsValid() = false, want true", c)
		}
	}

	if ResourceClass("bogus").IsValid() {
		t.Error(`ResourceClass("bogus").IsValid() = true, want false`)
	}
}

func TestResourceSubClassIsValid(t *testing.T) {
	valid := []ResourceSubClass{
		ResourceSubClassUndefined,
		ResourceSubClassUnknown,
		ResourceSubClassAzBlob,
		ResourceSubClassFTP,
		ResourceSubClassGCS,
		ResourceSubClassIMAP,
		ResourceSubClassMongoDB,
		ResourceSubClassMySQL,
		ResourceSubClassPVC,
		ResourceSubClassPostgreSQL,
		ResourceSubClassProxmox,
		ResourceSubClassRedis,
		ResourceSubClassS3,
		ResourceSubClassSFTP,
	}
	for _, c := range valid {
		if !c.IsValid() {
			t.Errorf("ResourceSubClass(%q).IsValid() = false, want true", c)
		}
	}

	if ResourceSubClass("bogus").IsValid() {
		t.Error(`ResourceSubClass("bogus").IsValid() = true, want false`)
	}
}

func TestConnectorTypeIsValid(t *testing.T) {
	valid := []ConnectorType{
		ConnectorTypeImporter,
		ConnectorTypeExporter,
		ConnectorTypeStorage,
		ConnectorTypeSecretProvider,
		ConnectorTypeInventory,
	}
	for _, c := range valid {
		if !c.IsValid() {
			t.Errorf("ConnectorType(%q).IsValid() = false, want true", c)
		}
	}

	if ConnectorType("bogus").IsValid() {
		t.Error(`ConnectorType("bogus").IsValid() = true, want false`)
	}
}

func TestIsSubClassOf(t *testing.T) {
	tests := []struct {
		sub    ResourceSubClass
		parent ResourceClass
		want   bool
	}{
		{ResourceSubClassS3, ResourceClassObjectStorage, true},
		{ResourceSubClassGCS, ResourceClassObjectStorage, true},
		{ResourceSubClassAzBlob, ResourceClassObjectStorage, true},
		{ResourceSubClassPostgreSQL, ResourceClassDatabase, true},
		{ResourceSubClassMySQL, ResourceClassDatabase, true},
		{ResourceSubClassProxmox, ResourceClassHypervisor, true},
		{ResourceSubClassPVC, ResourceClassBlockStorage, true},
		{ResourceSubClassSFTP, ResourceClassService, true},
		// negative cases
		{ResourceSubClassS3, ResourceClassDatabase, false},
		{ResourceSubClassProxmox, ResourceClassObjectStorage, false},
		{ResourceSubClassS3, ResourceClassAnalytics, false}, // parent with empty subclass list
		{ResourceSubClassS3, ResourceClass("bogus"), false}, // unknown parent
	}
	for _, tt := range tests {
		if got := tt.sub.IsSubClassOf(tt.parent); got != tt.want {
			t.Errorf("%q.IsSubClassOf(%q) = %v, want %v", tt.sub, tt.parent, got, tt.want)
		}
	}
}

func TestGetClassTree(t *testing.T) {
	tree := GetClassTree()
	if tree == nil {
		t.Fatal("GetClassTree() returned nil")
	}
	if _, ok := tree[ResourceClassObjectStorage]; !ok {
		t.Error("class tree missing object-storage")
	}
}

// Every class that appears as a key in the class tree must itself be a
// valid class, and every subclass listed under it must be a valid
// subclass. This guards against the kind of drift where a constant is
// added to the tree but not to the validity slice (and vice versa).
func TestClassTreeConsistency(t *testing.T) {
	for class, subs := range GetClassTree() {
		if !class.IsValid() {
			t.Errorf("class %q is a key in the tree but IsValid() = false", class)
		}
		for _, sub := range subs {
			if !sub.IsValid() {
				t.Errorf("subclass %q (under %q) IsValid() = false", sub, class)
			}
			if !sub.IsSubClassOf(class) {
				t.Errorf("%q listed under %q but IsSubClassOf() = false", sub, class)
			}
		}
	}
}
