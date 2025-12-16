package visualization

// VisualizationMode 全局可视化模式
// 支持的值: "ascii", "none"
// 未来可扩展: "json", "html" 等
var VisualizationMode = "ascii"

// Visualizable 接口：可被可视化的组件
// GetVisualState() 根据全局 VisualizationMode 返回对应格式的字符串
type Visualizable interface {
	GetVisualState() string
}
