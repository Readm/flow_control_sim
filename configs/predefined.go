package configs

import (
	"embed"
)

//go:embed json/*.json
var embeddedConfigs embed.FS

// NOTE: The init() function and registerEmbeddedJSON() have been disabled
// because the required packages (framework/app and configs/loader) have been removed.
// If you need to restore this functionality, you'll need to:
// 1. Restore or reimplement the framework/app package
// 2. Restore or reimplement the configs/loader package
// 3. Implement the Register() function
//
// func init() {
// 	if err := registerEmbeddedJSON(); err != nil {
// 		panic(fmt.Errorf("register embedded configs: %w", err))
// 	}
// }
//
// func registerEmbeddedJSON() error {
// 	entries, err := embeddedConfigs.ReadDir("json")
// 	if err != nil {
// 		return err
// 	}
// 	sort.Slice(entries, func(i, j int) bool {
// 		return entries[i].Name() < entries[j].Name()
// 	})
// 	for _, entry := range entries {
// 		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
// 			continue
// 		}
// 		data, err := embeddedConfigs.ReadFile(path.Join("json", entry.Name()))
// 		if err != nil {
// 			return fmt.Errorf("read embedded config %s: %w", entry.Name(), err)
// 		}
// 		doc, err := loader.Load(bytes.NewReader(data))
// 		if err != nil {
// 			return fmt.Errorf("parse embedded config %s: %w", entry.Name(), err)
// 		}
// 		cfg, err := doc.ToAppConfig()
// 		if err != nil {
// 			return fmt.Errorf("build config %s: %w", doc.Meta.Name, err)
// 		}
// 		Register(app.ConfigDescriptor{
// 			Name:        doc.Meta.Name,
// 			Description: doc.Meta.Description,
// 			Config:      cfg,
// 		})
// 	}
// 	return nil
// }
