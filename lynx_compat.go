package sentinel

import "github.com/go-lynx/lynx"

func currentLynxName() string {
	return lynx.GetName()
}
