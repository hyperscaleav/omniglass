package profile

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// Surface is a part of a protocol face that more than one device profile
// serves, defined once in surfaces/<name>.yaml: the subjects and commands a
// family of devices answer alike (a touch panel's room and source control,
// which a control processor answers too). An interface names the surfaces it
// serves in Include, and the loader expands them in front of the interface's
// own subjects and commands, so a device that answers more than another
// declares only the more.
type Surface struct {
	Subjects []Subject `yaml:"subjects"`
	Commands []Command `yaml:"commands"`
}

// loadSurfaces reads dir's surfaces, keyed by file name less its extension.
// A catalog with no surfaces directory has none.
func loadSurfaces(fsys fs.FS, dir string) (map[string]Surface, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || isFSNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := map[string]Surface{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		full := path.Join(dir, e.Name())
		data, err := fs.ReadFile(fsys, full)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", full, err)
		}
		var s Surface
		if err := yaml.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", full, err)
		}
		out[strings.TrimSuffix(e.Name(), ".yaml")] = s
	}
	return out, nil
}

// expandIncludes puts each interface's included surfaces ahead of its own
// subjects and commands. An include that names no surface fails, and so does
// an interface that restates a command or subject a surface defines: the
// restatement would be a copy that drifts from the definition it shadows.
func expandIncludes(p *DeviceProfile, surfaces map[string]Surface) error {
	for i := range p.Interfaces {
		iface := &p.Interfaces[i]
		if len(iface.Include) == 0 {
			continue
		}
		var subjects []Subject
		var commands []Command
		for _, name := range iface.Include {
			s, ok := surfaces[name]
			if !ok {
				return fmt.Errorf("interface %q includes surface %q, which the catalog does not define", iface.Name, name)
			}
			subjects = append(subjects, s.Subjects...)
			commands = append(commands, s.Commands...)
		}
		seenSubject, seenCommand := map[string]bool{}, map[string]bool{}
		for _, s := range subjects {
			if seenSubject[s.Match] {
				return fmt.Errorf("interface %q includes two surfaces that both define subject %q", iface.Name, s.Match)
			}
			seenSubject[s.Match] = true
		}
		for _, c := range commands {
			if seenCommand[c.Match] {
				return fmt.Errorf("interface %q includes two surfaces that both define command %q", iface.Name, c.Match)
			}
			seenCommand[c.Match] = true
		}
		for _, s := range iface.Subjects {
			if seenSubject[s.Match] {
				return fmt.Errorf("interface %q restates subject %q, which an included surface defines", iface.Name, s.Match)
			}
		}
		for _, c := range iface.Commands {
			if seenCommand[c.Match] {
				return fmt.Errorf("interface %q restates command %q, which an included surface defines", iface.Name, c.Match)
			}
		}
		iface.Subjects = append(subjects, iface.Subjects...)
		iface.Commands = append(commands, iface.Commands...)
	}
	return nil
}
