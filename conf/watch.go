package conf

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

func (p *Conf) Watch(filePath, sDnsPath string, reload func()) error {
	_, err := p.watchFiles(filePath, sDnsPath, reload, 5*time.Second)
	return err
}

func (p *Conf) watchFiles(filePath, sDnsPath string, reload func(), delay time.Duration) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("new watcher error: %s", err)
	}
	if err = watcher.Add(filePath); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("watch file error: %s", err)
	}
	if sDnsPath != "" {
		if err = watcher.Add(sDnsPath); err != nil {
			watcher.Close()
			return nil, fmt.Errorf("watch dns file error: %s", err)
		}
	}
	done := make(chan struct{})
	go func() {
		var pre time.Time
		defer watcher.Close()
		defer close(done)
		for {
			select {
			case e, ok := <-watcher.Events:
				if !ok {
					return
				}
				if e.Has(fsnotify.Chmod) {
					continue
				}
				if pre.Add(10 * time.Second).After(time.Now()) {
					continue
				}
				pre = time.Now()
				go func(e fsnotify.Event) {
					timer := time.NewTimer(delay)
					defer timer.Stop()
					select {
					case <-timer.C:
					case <-done:
						return
					}
					switch filepath.Base(strings.TrimSuffix(e.Name, "~")) {
					case filepath.Base(sDnsPath):
						log.Println("DNS file changed, reloading...")
					default:
						log.Println("config file changed, reloading...")
					}
					*p = *New()
					err := p.LoadFromPath(filePath)
					if err != nil {
						log.Printf("reload config error: %s", err)
					}
					reload()
					log.Println("reload config success")
				}(e)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				if err != nil {
					log.Printf("File watcher error: %s", err)
				}
			}
		}
	}()
	return watcher, nil
}
