package support

import (
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/sdougbrown/poo"
)

// ConfigErrorf is a failure in a transport's own configuration.
//
// It is a rejected request: the endpoint never saw it, and the run
// settles with the message.
func ConfigErrorf(format string, args ...any) error {
	return &poo.ProviderError{
		Kind:    poo.ProviderKindInvalidRequest,
		Message: fmt.Sprintf(format, args...),
	}
}

// ReasonError renders a configuration error for a request's failure.
//
// The error names the setting that is at fault, so the reader knows what
// to change.
func ReasonError(err error) string {
	var cfg *poo.ConfigError
	if errors.As(err, &cfg) {
		return cfg.Field + ": " + cfg.Message
	}
	return err.Error()
}

// SortedTools returns the tools in the order a request sends them.
//
// The order is by name, so two sessions that registered the same tools
// in different orders send the same array. The array is part of the
// prompt's prefix, and a prefix that changes breaks the endpoint's
// cache.
func SortedTools(specs []poo.ToolSpec) []poo.ToolSpec {
	out := make([]poo.ToolSpec, len(specs))
	copy(out, specs)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ZeroModelInfo reports that a request carried no model info.
//
// The comparison is the whole struct, so a field the model info grows is
// one the answer already covers.
func ZeroModelInfo(info poo.ModelInfo) bool {
	return reflect.DeepEqual(info, poo.ModelInfo{})
}

// ToolsMode returns how a run asks for tool calls.
//
// A model info that names a mode wins over the transport's configured
// one, because the model is what can make the calls.
func ToolsMode(info poo.ModelInfo, configured string) string {
	if mode := info.Tools.Mode; mode != "" {
		return string(mode)
	}
	return configured
}

// AttachmentDataURL renders an attachment as the data URL an image part
// takes.
func AttachmentDataURL(attachment poo.ContentBlock) string {
	return "data:" + attachment.MediaType + ";base64," + base64.StdEncoding.EncodeToString(attachment.Data)
}

// audioFormats are the media types an audio part may carry, by the
// format name the API takes.
var audioFormats = map[string]string{
	"audio/wav":  "wav",
	"audio/mpeg": "mp3",
}

// AudioFormat returns the format name an audio part takes for a media
// type. A type the API has no name for returns the empty string.
func AudioFormat(mediaType string) string {
	return audioFormats[mediaType]
}

// AudioMediaType names the media type a format stands for.
//
// A quirk that rewrites the part as a URL needs the media type back, and
// the format is the only name the part keeps.
func AudioMediaType(format string) string {
	for mediaType, name := range audioFormats {
		if name == format {
			return mediaType
		}
	}
	return ""
}
