package llm

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	volundpb "github.com/ai-volund/volund-proto/gen/go/volund/v1"
)

// Service implements the gRPC LLMService by delegating to the Router.
type Service struct {
	volundpb.UnimplementedLLMServiceServer
	router *Router
}

// NewService creates an LLM gRPC service backed by the given router.
func NewService(r *Router) *Service {
	return &Service{router: r}
}

func (s *Service) Chat(ctx context.Context, req *volundpb.ChatRequest) (*volundpb.ChatResponse, error) {
	pr, err := protoToProviderRequest(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid request: %v", err)
	}
	resp, err := s.router.Chat(ctx, req.Provider, pr)
	if err != nil {
		slog.Error("llm router chat error", "error", err, "provider", req.Provider)
		return nil, status.Errorf(codes.Internal, "llm error: %v", err)
	}
	return providerResponseToProto(resp), nil
}

func (s *Service) StreamChat(req *volundpb.StreamChatRequest, stream volundpb.LLMService_StreamChatServer) error {
	pr := streamProtoToProviderRequest(req)
	st, err := s.router.StreamChat(stream.Context(), req.Provider, pr)
	if err != nil {
		return status.Errorf(codes.Internal, "llm stream error: %v", err)
	}
	defer st.Close() //nolint:errcheck

	for {
		chunk, err := st.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return status.Errorf(codes.Internal, "stream recv: %v", err)
		}
		pbChunk, err := chunkToProto(chunk)
		if err != nil {
			return status.Errorf(codes.Internal, "chunk encode: %v", err)
		}
		if err := stream.Send(pbChunk); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Embed(ctx context.Context, req *volundpb.EmbedRequest) (*volundpb.EmbedResponse, error) {
	vecs, usage, err := s.router.Embed(ctx, "", req.Model, req.Texts)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "embed error: %v", err)
	}
	var embeddings []*volundpb.Embedding
	for _, v := range vecs {
		embeddings = append(embeddings, &volundpb.Embedding{
			Values:     v,
			Dimensions: int32(len(v)),
		})
	}
	var pbUsage *volundpb.UsageInfo
	if usage != nil {
		pbUsage = &volundpb.UsageInfo{InputTokens: usage.InputTokens}
	}
	return &volundpb.EmbedResponse{Embeddings: embeddings, Usage: pbUsage}, nil
}

func (s *Service) ListModels(ctx context.Context, req *volundpb.ListModelsRequest) (*volundpb.ListModelsResponse, error) {
	models, err := s.router.ListModels(ctx, req.Provider)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list models: %v", err)
	}
	var pbModels []*volundpb.ModelInfo
	for _, m := range models {
		pbModels = append(pbModels, &volundpb.ModelInfo{
			Provider:           m.Provider,
			ModelId:            m.ID,
			DisplayName:        m.DisplayName,
			ContextWindow:      m.ContextWindow,
			MaxOutputTokens:    m.MaxOutputTokens,
			SupportsVision:     m.SupportsVision,
			SupportsToolUse:    m.SupportsToolUse,
			SupportsStreaming:   m.SupportsStreaming,
			InputPricePerMtok:  m.InputPricePerMTok,
			OutputPricePerMtok: m.OutputPricePerMTok,
		})
	}
	return &volundpb.ListModelsResponse{Models: pbModels}, nil
}

// ── proto ↔ internal conversions ─────────────────────────────────────────────

func protoToProviderRequest(req *volundpb.ChatRequest) (*ProviderRequest, error) {
	msgs, err := protoMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	return &ProviderRequest{
		Model:         req.Model,
		Messages:      msgs,
		Temperature:   req.Temperature,
		MaxTokens:     req.MaxTokens,
		StopSequences: req.StopSequences,
		Tools:         protoTools(req.Tools),
	}, nil
}

func streamProtoToProviderRequest(req *volundpb.StreamChatRequest) *ProviderRequest {
	msgs, _ := protoMessages(req.Messages)
	return &ProviderRequest{
		Model:         req.Model,
		Messages:      msgs,
		Temperature:   req.Temperature,
		MaxTokens:     req.MaxTokens,
		StopSequences: req.StopSequences,
		Tools:         protoTools(req.Tools),
	}
}

func protoMessages(pbMsgs []*volundpb.LLMMessage) ([]*Message, error) {
	msgs := make([]*Message, 0, len(pbMsgs))
	for _, m := range pbMsgs {
		blocks, err := protoBlocks(m.Content)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, &Message{
			Role:       m.Role,
			Content:    blocks,
			ToolCallID: m.ToolCallId,
		})
	}
	return msgs, nil
}

func protoBlocks(pbBlocks []*volundpb.ContentBlock) ([]*Block, error) {
	blocks := make([]*Block, 0, len(pbBlocks))
	for _, b := range pbBlocks {
		switch v := b.Block.(type) {
		case *volundpb.ContentBlock_Text:
			blocks = append(blocks, &Block{Type: "text", Text: v.Text})
		case *volundpb.ContentBlock_Image:
			blocks = append(blocks, &Block{
				Type: "image",
				Image: &ImageData{
					MediaType: v.Image.MediaType,
					Data:      v.Image.Data,
					URL:       v.Image.Url,
				},
			})
		case *volundpb.ContentBlock_ToolUse:
			blocks = append(blocks, &Block{
				Type: "tool_use",
				ToolUse: &ToolUseData{
					ID:        v.ToolUse.Id,
					Name:      v.ToolUse.Name,
					InputJSON: v.ToolUse.InputJson,
				},
			})
		case *volundpb.ContentBlock_ToolResult:
			blocks = append(blocks, &Block{
				Type: "tool_result",
				ToolResult: &ToolResultData{
					ToolUseID: v.ToolResult.ToolUseId,
					Content:   v.ToolResult.Content,
					IsError:   v.ToolResult.IsError,
				},
			})
		default:
			return nil, fmt.Errorf("unknown content block type %T", b.Block)
		}
	}
	return blocks, nil
}

func protoTools(pbTools []*volundpb.ToolDefinition) []*ToolDef {
	tools := make([]*ToolDef, len(pbTools))
	for i, t := range pbTools {
		tools[i] = &ToolDef{
			Name:            t.Name,
			Description:     t.Description,
			InputSchemaJSON: t.InputSchemaJson,
		}
	}
	return tools
}

func providerResponseToProto(resp *ProviderResponse) *volundpb.ChatResponse {
	return &volundpb.ChatResponse{
		Id:         resp.ID,
		Provider:   "openai",
		Model:      resp.Model,
		Content:    blocksToProto(resp.Content),
		StopReason: resp.StopReason,
		Usage: &volundpb.UsageInfo{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		},
	}
}

func blocksToProto(blocks []*Block) []*volundpb.ContentBlock {
	pb := make([]*volundpb.ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			pb = append(pb, &volundpb.ContentBlock{
				Block: &volundpb.ContentBlock_Text{Text: b.Text},
			})
		case "tool_use":
			if b.ToolUse != nil {
				pb = append(pb, &volundpb.ContentBlock{
					Block: &volundpb.ContentBlock_ToolUse{
						ToolUse: &volundpb.ToolUseContent{
							Id:        b.ToolUse.ID,
							Name:      b.ToolUse.Name,
							InputJson: b.ToolUse.InputJSON,
						},
					},
				})
			}
		}
	}
	return pb
}

func chunkToProto(c *Chunk) (*volundpb.StreamChatResponse, error) {
	if c.Final != nil {
		return &volundpb.StreamChatResponse{
			Chunk: &volundpb.StreamChatResponse_Complete{
				Complete: providerResponseToProto(c.Final),
			},
		}, nil
	}
	if c.ToolUse != nil {
		return &volundpb.StreamChatResponse{
			Chunk: &volundpb.StreamChatResponse_ToolUse{
				ToolUse: &volundpb.ToolUseContent{
					Id:        c.ToolUse.ID,
					Name:      c.ToolUse.Name,
					InputJson: c.ToolUse.InputJSON,
				},
			},
		}, nil
	}
	return &volundpb.StreamChatResponse{
		Chunk: &volundpb.StreamChatResponse_TextDelta{TextDelta: c.TextDelta},
	}, nil
}
