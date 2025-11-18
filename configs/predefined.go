package configs

import (
	"bytes"
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"

	app "github.com/Readm/flow_sim/framework/app"

	"github.com/Readm/flow_sim/configs/loader"
)

//go:embed json/*.json
var embeddedConfigs embed.FS

func init() {
	if err := registerEmbeddedJSON(); err != nil {
		panic(fmt.Errorf("register embedded configs: %w", err))
	}
}

func registerEmbeddedJSON() error {
	entries, err := embeddedConfigs.ReadDir("json")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := embeddedConfigs.ReadFile(path.Join("json", entry.Name()))
		if err != nil {
			return fmt.Errorf("read embedded config %s: %w", entry.Name(), err)
		}
		doc, err := loader.Load(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("parse embedded config %s: %w", entry.Name(), err)
		}
		cfg, err := doc.ToAppConfig()
		if err != nil {
			return fmt.Errorf("build config %s: %w", doc.Meta.Name, err)
		}
		Register(app.ConfigDescriptor{
			Name:        doc.Meta.Name,
			Description: doc.Meta.Description,
			Config:      cfg,
		})
	}
	return nil
}
