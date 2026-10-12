package cloudinit

import (
	"bytes"
	"errors"
	"io"
	"net"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var errCloudConfig = errors.New("unsupported or invalid cloud-config")

var dnsLabel = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func validConfig(config Config) bool {
	if !ValidAgentVersion(config.AgentVersion) {
		return false
	}
	if strings.TrimSpace(config.License) == "" || len(config.License) > 8192 || config.Port < 0 || config.Port > 65535 || config.Host == "" || len(config.Host) > 4096 {
		return false
	}
	for _, value := range config.License {
		if value < 32 || value > 126 {
			return false
		}
	}
	for _, host := range strings.Split(config.Host, "/") {
		if net.ParseIP(host) != nil {
			continue
		}
		if len(host) > 253 {
			return false
		}
		for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
			if !dnsLabel.MatchString(label) {
				return false
			}
		}
	}
	return true
}

func parseCloudConfig(data []byte) (*yaml.Node, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, nil
	}
	if len(data) > 1024*1024 || !(bytes.HasPrefix(data, []byte("#cloud-config\n")) || bytes.HasPrefix(data, []byte("#cloud-config\r\n"))) {
		return nil, errCloudConfig
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document, extra yaml.Node
	decodeErr := decoder.Decode(&document)
	if decodeErr == io.EOF {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, nil
	}
	if decodeErr != nil || len(document.Content) != 1 || decoder.Decode(&extra) != io.EOF {
		return nil, errCloudConfig
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode || !validNode(root, 0) {
		return nil, errCloudConfig
	}
	var validated interface{}
	if root.Decode(&validated) != nil {
		return nil, errCloudConfig
	}
	for i := 0; i < len(root.Content); i += 2 {
		key, value := root.Content[i].Value, root.Content[i+1]
		switch key {
		case "merge_how", "merge_type", "cloud_init_modules", "cloud_config_modules", "cloud_final_modules":
			return nil, errCloudConfig
		case "write_files", "runcmd":
			if value.Kind != yaml.SequenceNode {
				return nil, errCloudConfig
			}
			for _, entry := range value.Content {
				if key == "write_files" && !validFile(entry) || key == "runcmd" && !validCommand(entry) {
					return nil, errCloudConfig
				}
			}
		}
	}
	return root, nil
}

// Reject aliases and merge semantics rather than silently altering how cloud-init
// interprets a document. Inspect nodes before decoding so duplicates cannot hide.
func validNode(node *yaml.Node, depth int) bool {
	if depth > 64 || node.Anchor != "" || node.Kind == yaml.AliasNode {
		return false
	}
	switch node.Tag {
	case "!!map", "!!seq", "!!str", "!!null", "!!bool", "!!int", "!!float", "!!timestamp":
	default:
		return false
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]bool)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" || seen[key.Value] {
				return false
			}
			seen[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if !validNode(child, depth+1) {
			return false
		}
	}
	return true
}

func validFile(node *yaml.Node) bool {
	if node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value != "path" {
			continue
		}
		value := node.Content[i+1]
		if value.Tag != "!!str" || !strings.HasPrefix(value.Value, "/") || strings.ContainsAny(value.Value, "\x00\r\n") {
			return false
		}
		filePath := path.Clean(value.Value)
		for _, reserved := range []string{scriptPath, "/etc/whatap-kubevirt", "/var/lib/whatap-kubevirt", "/usr/whatap/infra", "/etc/whatap/infra.meta"} {
			if filePath == "/" || filePath == reserved || strings.HasPrefix(filePath, reserved+"/") || strings.HasPrefix(reserved, filePath+"/") {
				return false
			}
		}
		return true
	}
	return false
}

func validCommand(node *yaml.Node) bool {
	if node.Kind == yaml.ScalarNode {
		return node.Tag == "!!str"
	}
	if node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return false
	}
	for _, arg := range node.Content {
		if arg.Kind != yaml.ScalarNode || arg.Tag != "!!str" {
			return false
		}
	}
	return true
}
