package service

import (
	"fmt"
	"io/fs"
	"reflect"
	"strings"
)

// filesystemIdentity returns stable, cheap stat fields without importing a
// platform-specific syscall type. Unix implementations expose device/inode
// plus ctime; Windows exposes volume/file-index fields. Access time is
// intentionally excluded because merely probing a file may change it.
func filesystemIdentity(info fs.FileInfo) string {
	if info == nil || info.Sys() == nil {
		return ""
	}
	value := reflect.ValueOf(info.Sys())
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return ""
	}

	parts := make([]string, 0, 7)
	appendField := func(name string) {
		field := value.FieldByName(name)
		if !field.IsValid() || !field.CanInterface() {
			return
		}
		parts = append(parts, name+"="+fmt.Sprint(field.Interface()))
	}
	for _, name := range []string{
		"Dev", "Ino", "VolumeSerialNumber", "FileIndexHigh", "FileIndexLow",
		"Ctim", "Ctimespec", "ChangeTime", "CreationTime", "Birthtime",
	} {
		appendField(name)
	}
	if len(parts) == 0 {
		return ""
	}
	return reflect.TypeOf(info.Sys()).String() + ":" + strings.Join(parts, ",")
}
