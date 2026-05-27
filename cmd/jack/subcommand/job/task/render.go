package task

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/kpetremann/jackadi/cmd/jack/option"
	"github.com/kpetremann/jackadi/cmd/jack/style"
	"github.com/kpetremann/jackadi/internal/proto"
	"github.com/kpetremann/jackadi/internal/serializer"
)

func printTaskResult(responses *proto.FwdResponse) {
	var sb strings.Builder
	allResponses := responses.GetResponses()
	keys := maps.Keys(allResponses)
	if option.GetSortOutput() {
		keys = slices.Values(slices.Sorted(keys))
	}

	for id := range keys {
		res, ok := allResponses[id]
		if !ok || res == nil {
			continue
		}

		sb.WriteString(style.Title(id))
		if res.InternalError > 0 {
			sb.WriteString(style.InlineBlockTitle("id") + fmt.Sprintf("%d", res.GetId()))
			sb.WriteString(style.InlineBlockTitle("groupID") + fmt.Sprintf("%d", res.GetGroupID()))
			sb.WriteString(style.InlineBlockTitle("internal error") + style.ErrorStyle.Render(res.GetInternalError().String()))
			if res.GetModuleError() != "" {
				sb.WriteString(style.Block(res.GetModuleError()))
			}
			sb.WriteString("\n")
			continue
		}

		if res.GetOutput() != nil {
			sb.WriteString(style.BlockTitle("output"))
			var parsed any
			if err := serializer.JSON.Unmarshal(res.GetOutput(), &parsed); err != nil {
				sb.WriteString(style.Block(string(res.GetOutput())))
			}
			out, _ := yaml.MarshalWithOptions(parsed, yaml.UseLiteralStyleIfMultiline(true))
			sb.WriteString(style.Block(string(out)))
		} else {
			sb.WriteString(style.InlineBlockTitle("output"))
			sb.WriteString(style.Emph("empty"))
		}

		if res.GetError() != "" {
			sb.WriteString(style.BlockTitle("err"))
			sb.WriteString(style.Block(res.GetError()))
		}

		if res.GetRetcode() > 0 {
			sb.WriteString(style.InlineBlockTitle("retcode"))
			fmt.Fprintf(&sb, "%d", res.GetRetcode())
		}

		sb.WriteString("\n")
	}

	style.PrettyPrint(sb.String())
}
