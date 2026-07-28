package main

import (
	"flag"
	"fmt"
	"log"
	"sort"

	"auto-deploy-platform/internal/config"
)

func main() {
	path := flag.String("config", "config.yaml", "待检查或迁移的配置文件")
	write := flag.Bool("write", false, "校验通过后将配置写为最新版本")
	flag.Parse()

	cfg, err := config.Load(*path)
	if err != nil {
		log.Fatalf("配置校验失败: %v", err)
	}

	projectKeys := make([]string, 0, len(cfg.Projects))
	for key := range cfg.Projects {
		projectKeys = append(projectKeys, key)
	}
	sort.Strings(projectKeys)
	for _, key := range projectKeys {
		project := cfg.Projects[key]
		fmt.Printf("%s: mode=%s components=%d environments=%d\n", key, project.Mode, len(project.Components), len(project.Environments))
	}

	if !*write {
		fmt.Printf("配置有效，已按版本 %d 模型解析；未写入文件\n", config.CurrentVersion)
		return
	}
	manager, err := config.NewManager(*path)
	if err != nil {
		log.Fatalf("创建配置管理器失败: %v", err)
	}
	if err := manager.Update(cfg); err != nil {
		log.Fatalf("写入迁移配置失败: %v", err)
	}
	fmt.Printf("配置已迁移到版本 %d\n", config.CurrentVersion)
}
