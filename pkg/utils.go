package pkg

import (
	"encoding/json"
	"fmt"
)

func DebugJson(value interface{}) {
	prettyJSON, _ := json.MarshalIndent(value, "", "    ")
	fmt.Printf("%s\n", string(prettyJSON))
}
