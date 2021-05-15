package backend

import (
	"encoding/json"
	"k8s.io/klog/v2"
)

const (
	FakeMetaDataSnapshotKey = iota
	MetaDataKeyTargets
)

type SnapshotMetadataKey int
type SnapshotMetadata map[SnapshotMetadataKey]interface{}

func (m SnapshotMetadata) GetSnapshotKey() SnapshotKey {
	switch v := m[FakeMetaDataSnapshotKey].(type) {
	case SnapshotKey:
		return v
	case string:
		return SnapshotKey(v)
	default:
		panic(m[FakeMetaDataSnapshotKey])
	}
}

func (m SnapshotMetadata) SetSnapshotKey(key string) {
	m[FakeMetaDataSnapshotKey] = SnapshotKey(key)
}

func (m SnapshotMetadata) GetTargets() map[MountTarget]struct{} {
	switch v := m[MetaDataKeyTargets].(type) {
	case map[MountTarget]struct{}:
		return v
	case map[string]interface{}:
		r := make(map[MountTarget]struct{}, len(v))
		for k := range v {
			r[MountTarget(k)] = struct{}{}
		}

		return r
	default:
		panic(m[MetaDataKeyTargets])
	}
}

func (m SnapshotMetadata) SetTargets(targets map[MountTarget]struct{}) {
	m[MetaDataKeyTargets] = targets
}

func (m SnapshotMetadata) CopyTargets(targets map[MountTarget]struct{}) {
	metaTargets := m[MetaDataKeyTargets].(map[MountTarget]struct{})
	for target := range targets {
		metaTargets[target] = struct{}{}
	}
}

func (m SnapshotMetadata) Encode() string {
	bytes, err := json.Marshal(m)
	if err != nil {
		klog.Fatalf("unable to encode snapshot metadata: %s", err)
	}

	return string(bytes)
}

func (m SnapshotMetadata) Decode(encoded string) error {
	if err := json.Unmarshal([]byte(encoded), &m); err != nil {
		klog.Errorf("unable to decode snapshot metadata: %s", err)
		return err
	}
	return nil
}

func createSnapshotMetaData(target MountTarget) SnapshotMetadata {
	return SnapshotMetadata{
		MetaDataKeyTargets: map[MountTarget]struct{}{target:{}},
	}
}

func buildSnapshotMetaData(targets map[MountTarget]struct{}) SnapshotMetadata {
	return SnapshotMetadata{
		MetaDataKeyTargets: targets,
	}
}
