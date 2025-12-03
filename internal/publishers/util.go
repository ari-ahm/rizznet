package publishers

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	
	"rizznet/internal/logger"
	"rizznet/internal/model"
	"rizznet/internal/xray/parser"
)

func GenerateSubscriptionPayload(categories []model.Category, config map[string]interface{}) (string, error) {
	
	// Map Hash -> Profile (so we can merge categories)
	uniqueProfiles := make(map[string]*parser.Profile)
	// Map Hash -> List of Categories
	profileCats := make(map[string][]string)
	// Map Hash -> Proxy Model (for country/dirt info)
	profileMeta := make(map[string]model.Proxy)
	
	for _, cat := range categories {
		for _, proxy := range cat.Proxies {
			if _, exists := uniqueProfiles[proxy.Hash]; !exists {
				p, err := parser.Parse(proxy.Raw)
				if err != nil {
					logger.Log.Debugf("⚠️ Publisher dropped link (Parse Error): %s... | Err: %v", proxy.Raw[:20], err)
					continue
				}
				uniqueProfiles[proxy.Hash] = p
				profileMeta[proxy.Hash] = proxy
			}
			// Append category name if not already present
			currentCats := profileCats[proxy.Hash]
			found := false
			for _, c := range currentCats {
				if c == cat.Name { found = true; break }
			}
			if !found {
				profileCats[proxy.Hash] = append(profileCats[proxy.Hash], cat.Name)
			}
		}
	}

	// Build final lines
	var lines []string
	for hash, p := range uniqueProfiles {
		meta := profileMeta[hash]
		cats := profileCats[hash]
		sort.Strings(cats) // Sort categories for consistent remarks
		catStr := strings.Join(cats, "|")

		flag := getFlagEmoji(meta.Country)
		
		metaFlags := ""
		if meta.IsRotating {
			metaFlags += " ♻️"
		}
		if meta.IsDirty {
			metaFlags += " ⚠️"
		}

		// New Format: [Flag] [MetaFlags] [Country] [Cat1|Cat2]
		p.Remarks = fmt.Sprintf("%s%s %s %s", flag, metaFlags, meta.Country, catStr)
		
		lines = append(lines, p.ToURI())
	}

	finalText := strings.Join(lines, "\n")

	useBase64, _ := config["base64"].(bool)
	if useBase64 {
		return base64.StdEncoding.EncodeToString([]byte(finalText)), nil
	}

	return finalText, nil
}

func getFlagEmoji(countryCode string) string {
	if len(countryCode) != 2 {
		return "🌐"
	}
	countryCode = strings.ToUpper(countryCode)
	return string(rune(countryCode[0])+127397) + string(rune(countryCode[1])+127397)
}
