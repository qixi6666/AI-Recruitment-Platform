package ai

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	einoOpenAI "github.com/cloudwego/eino-ext/components/embedding/openai"
	einoMilvus "github.com/cloudwego/eino-ext/components/retriever/milvus2"
	einoSearchMode "github.com/cloudwego/eino-ext/components/retriever/milvus2/search_mode"
	einoembedding "github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
	"github.com/milvus-io/milvus/client/v2/entity"
	milvusindex "github.com/milvus-io/milvus/client/v2/index"
	milvus "github.com/milvus-io/milvus/client/v2/milvusclient"
	"golang.org/x/sync/errgroup"

	"recruitment/logic-grpc-service/internal/config"
	"recruitment/logic-grpc-service/internal/domain"
)

const (
	resumeChunkSize                 = 900
	resumeMaxChunks                 = 80
	embeddingBatchSize              = 16
	recommendationSemanticWeight    = 0.65
	recommendationKeywordWeight     = 0.35
	recommendationTopEvidenceWeight = 0.70
)

type resumeRAGSearcher struct {
	cfg      config.RAGConfig
	embedder einoembedding.Embedder
}

type resumeChunkRow struct {
	ChunkID   int64 `milvus:"name:chunk_id"`
	HRID      int64 `milvus:"name:hr_id"`
	JobID     int64 `milvus:"name:job_id"`
	CreatedAt int64 `milvus:"name:created_at"`
}

type resumeExperienceChunk struct {
	SectionType     string
	SectionTitle    string
	ExperienceIndex int
	ChunkIndex      int
	Content         string
	SearchText      string
}

func (c *Client) ragEnabled() bool {
	rag := c.cfg.RAG
	return rag.Enabled && rag.EmbeddingAPIKey != "" && rag.MilvusAddress != "" && rag.MilvusCollection != ""
}

func (c *Client) IndexApplicationResume(ctx context.Context, hrID, jobID, applicationID, candidateID uint64, resume domain.Resume, profile domain.CandidateProfile, jobTitle string) error {
	if !c.ragEnabled() {
		return nil
	}
	searcher, err := newResumeRAGSearcher(ctx, c.cfg.RAG)
	if err != nil {
		return err
	}
	chunks := buildExperienceChunksFromProfile(profile)
	if len(chunks) == 0 {
		return fmt.Errorf("candidate profile has no project/work experience chunks")
	}
	texts := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		texts = append(texts, chunk.Content)
	}
	embeddings, err := searcher.embedDocuments(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed resume chunks: %w", err)
	}
	if len(embeddings) != len(chunks) {
		return fmt.Errorf("embedding count mismatch: got %d vectors for %d chunks", len(embeddings), len(chunks))
	}
	dim, err := vectorDim(embeddings)
	if err != nil {
		return err
	}

	milvusClient, err := searcher.newMilvusClient(ctx)
	if err != nil {
		return err
	}
	defer milvusClient.Close(ctx)

	oldChunkIDs, err := c.resumeExperienceChunkIDs(ctx, hrID, jobID, applicationID)
	if err != nil {
		return err
	}
	if len(oldChunkIDs) > 0 {
		deleteExpr := fmt.Sprintf("chunk_id in [%s]", joinUint64s(oldChunkIDs))
		if _, err := milvusClient.Delete(ctx, milvus.NewDeleteOption(searcher.cfg.MilvusCollection).WithExpr(deleteExpr)); err != nil {
			return fmt.Errorf("delete old resume chunk vectors: %w", err)
		}
		if err := c.db.WithContext(ctx).
			Where("id IN ?", oldChunkIDs).
			Delete(&domain.ResumeExperienceChunk{}).Error; err != nil {
			return fmt.Errorf("delete old resume chunks: %w", err)
		}
	}

	count := len(chunks)
	rows := make([]domain.ResumeExperienceChunk, count)
	for i, chunk := range chunks {
		rows[i] = domain.ResumeExperienceChunk{
			HRID:            hrID,
			JobID:           jobID,
			ApplicationID:   applicationID,
			ResumeID:        resume.ID,
			CandidateID:     candidateID,
			SectionType:     chunk.SectionType,
			SectionTitle:    chunk.SectionTitle,
			ExperienceIndex: chunk.ExperienceIndex,
			ChunkIndex:      chunk.ChunkIndex,
			KeywordText:     chunk.SearchText,
			EvidenceText:    chunk.Content,
			OriginalText:    chunk.Content,
		}
	}
	if err := c.db.WithContext(ctx).Create(&rows).Error; err != nil {
		return fmt.Errorf("create resume experience chunks: %w", err)
	}

	chunkIDs := make([]int64, count)
	hrIDs := make([]int64, count)
	jobIDs := make([]int64, count)
	searchTexts := make([]string, count)
	createdAt := make([]int64, count)
	now := time.Now().Unix()
	for i, chunk := range chunks {
		chunkIDs[i] = int64(rows[i].ID)
		hrIDs[i] = int64(hrID)
		jobIDs[i] = int64(jobID)
		searchTexts[i] = chunk.SearchText
		createdAt[i] = now
	}
	_, err = milvusClient.Insert(
		ctx,
		milvus.NewColumnBasedInsertOption(searcher.cfg.MilvusCollection).
			WithInt64Column("chunk_id", chunkIDs).
			WithInt64Column("hr_id", hrIDs).
			WithInt64Column("job_id", jobIDs).
			WithVarcharColumn(searcher.cfg.MilvusTextField, searchTexts).
			WithInt64Column("created_at", createdAt).
			WithFloatVectorColumn(searcher.cfg.MilvusVectorField, dim, embeddings),
	)
	if err != nil {
		_ = c.db.WithContext(ctx).Where("id IN ?", chunkIDs).Delete(&domain.ResumeExperienceChunk{}).Error
		return fmt.Errorf("insert resume chunks: %w", err)
	}
	return nil
}

func (c *Client) resumeExperienceChunkIDs(ctx context.Context, hrID, jobID, applicationID uint64) ([]uint64, error) {
	var rows []domain.ResumeExperienceChunk
	if err := c.db.WithContext(ctx).
		Select("id").
		Where("hr_id = ? AND job_id = ? AND application_id = ?", hrID, jobID, applicationID).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("find old resume chunks: %w", err)
	}
	ids := make([]uint64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

func (s *resumeRAGSearcher) newMilvusClient(ctx context.Context) (*milvus.Client, error) {
	client, err := milvus.New(ctx, &milvus.ClientConfig{
		Address:  s.cfg.MilvusAddress,
		Username: s.cfg.MilvusUsername,
		Password: s.cfg.MilvusPassword,
		DBName:   s.cfg.MilvusDBName,
	})
	if err != nil {
		return nil, fmt.Errorf("connect milvus: %w", err)
	}
	if err := s.ensureResumeCollection(ctx, client); err != nil {
		client.Close(ctx)
		return nil, err
	}
	return client, nil
}

func (s *resumeRAGSearcher) ensureResumeCollection(ctx context.Context, client *milvus.Client) error {
	exists, err := client.HasCollection(ctx, milvus.NewHasCollectionOption(s.cfg.MilvusCollection))
	if err != nil {
		return fmt.Errorf("check milvus collection: %w", err)
	}
	if exists {
		return nil
	}
	if err := s.createResumeCollection(ctx, client); err != nil {
		return err
	}
	if err := s.createResumeCollectionIndexes(ctx, client); err != nil {
		return err
	}
	loadTask, err := client.LoadCollection(ctx, milvus.NewLoadCollectionOption(s.cfg.MilvusCollection))
	if err != nil {
		return fmt.Errorf("load milvus collection: %w", err)
	}
	if err := loadTask.Await(ctx); err != nil {
		return fmt.Errorf("await milvus collection load: %w", err)
	}
	return nil
}

func (s *resumeRAGSearcher) createResumeCollection(ctx context.Context, client *milvus.Client) error {
	textField := entity.NewField().
		WithName(s.cfg.MilvusTextField).
		WithDataType(entity.FieldTypeVarChar).
		WithMaxLength(8192).
		WithEnableAnalyzer(true).
		WithAnalyzerParams(map[string]any{"type": "chinese"})

	schema := entity.NewSchema().
		WithAutoID(true).
		WithField(entity.NewField().WithName("id").WithDataType(entity.FieldTypeInt64).WithIsPrimaryKey(true).WithIsAutoID(true)).
		WithField(entity.NewField().WithName("chunk_id").WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName("hr_id").WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName("job_id").WithDataType(entity.FieldTypeInt64)).
		WithField(textField).
		WithField(entity.NewField().WithName(s.cfg.MilvusVectorField).WithDataType(entity.FieldTypeFloatVector).WithDim(int64(s.cfg.EmbeddingDimension))).
		WithField(entity.NewField().WithName(s.cfg.MilvusSparseVectorField).WithDataType(entity.FieldTypeSparseVector)).
		WithField(entity.NewField().WithName("created_at").WithDataType(entity.FieldTypeInt64)).
		WithFunction(entity.NewFunction().
			WithName("resume_search_text_bm25").
			WithType(entity.FunctionTypeBM25).
			WithInputFields(s.cfg.MilvusTextField).
			WithOutputFields(s.cfg.MilvusSparseVectorField))

	if err := client.CreateCollection(ctx, milvus.NewCreateCollectionOption(s.cfg.MilvusCollection, schema)); err != nil {
		return fmt.Errorf("create milvus collection: %w", err)
	}
	return nil
}

func (s *resumeRAGSearcher) createResumeCollectionIndexes(ctx context.Context, client *milvus.Client) error {
	metricType := entity.MetricType(strings.ToUpper(s.cfg.MilvusMetricType))
	if metricType == "" {
		metricType = entity.COSINE
	}
	denseTask, err := client.CreateIndex(
		ctx,
		milvus.NewCreateIndexOption(
			s.cfg.MilvusCollection,
			s.cfg.MilvusVectorField,
			milvusindex.NewHNSWIndex(metricType, 16, 200),
		),
	)
	if err != nil {
		return fmt.Errorf("create dense vector index: %w", err)
	}
	if err := denseTask.Await(ctx); err != nil {
		return fmt.Errorf("await dense vector index: %w", err)
	}

	sparseTask, err := client.CreateIndex(
		ctx,
		milvus.NewCreateIndexOption(
			s.cfg.MilvusCollection,
			s.cfg.MilvusSparseVectorField,
			milvusindex.NewSparseInvertedIndex(entity.BM25, 0),
		),
	)
	if err != nil {
		return fmt.Errorf("create sparse vector index: %w", err)
	}
	if err := sparseTask.Await(ctx); err != nil {
		return fmt.Errorf("await sparse vector index: %w", err)
	}
	return nil
}

func (s *resumeRAGSearcher) newMilvusRetriever(ctx context.Context, client *milvus.Client, topK int, mode einoMilvus.SearchMode) (*einoMilvus.Retriever, error) {
	retriever, err := einoMilvus.NewRetriever(ctx, &einoMilvus.RetrieverConfig{
		Client:            client,
		Collection:        s.cfg.MilvusCollection,
		VectorField:       s.cfg.MilvusVectorField,
		SparseVectorField: s.cfg.MilvusSparseVectorField,
		OutputFields:      s.cfg.MilvusOutputFields,
		TopK:              topK,
		SearchMode:        mode,
		DocumentConverter: resumeDocumentConverter,
		Embedding:         s.embedder,
	})
	if err != nil {
		return nil, fmt.Errorf("create milvus retriever: %w", err)
	}
	return retriever, nil
}

func newResumeRAGSearcher(ctx context.Context, cfg config.RAGConfig) (*resumeRAGSearcher, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("resume RAG is disabled")
	}
	if cfg.EmbeddingAPIKey == "" {
		return nil, fmt.Errorf("rag embedding api key is required")
	}
	if cfg.MilvusAddress == "" || cfg.MilvusCollection == "" || cfg.MilvusVectorField == "" {
		return nil, fmt.Errorf("milvus rag config is incomplete")
	}
	if cfg.EmbeddingEndpoint == "" {
		cfg.EmbeddingEndpoint = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = "text-embedding-v3"
	}
	if cfg.EmbeddingDimension <= 0 {
		cfg.EmbeddingDimension = 1024
	}
	if cfg.EmbeddingTimeoutSeconds <= 0 {
		cfg.EmbeddingTimeoutSeconds = int64((15 * time.Second).Seconds())
	}
	if cfg.MilvusMetricType == "" {
		cfg.MilvusMetricType = string(entity.COSINE)
	}
	if cfg.MilvusSparseVectorField == "" {
		cfg.MilvusSparseVectorField = "sparse_vector"
	}
	if cfg.MilvusTextField == "" {
		cfg.MilvusTextField = "search_text"
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 8
	}
	if cfg.TopK > 20 {
		cfg.TopK = 20
	}
	if len(cfg.MilvusOutputFields) == 0 {
		cfg.MilvusOutputFields = []string{
			"chunk_id", "hr_id", "job_id", "created_at",
		}
	}
	embedder, err := newDashScopeOpenAIEmbedder(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &resumeRAGSearcher{cfg: cfg, embedder: embedder}, nil
}

func (s *resumeRAGSearcher) Search(ctx context.Context, hrID uint64, input ResumeSemanticSearchInput) ([]ResumeSemanticSearchItem, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return nil, fmt.Errorf("semantic search query is required")
	}
	milvusClient, err := s.newMilvusClient(ctx)
	if err != nil {
		return nil, err
	}
	defer milvusClient.Close(ctx)

	topK := normalizeRAGLimit(input.Limit, s.cfg.TopK)
	retriever, err := s.newMilvusRetriever(ctx, milvusClient, topK, einoSearchMode.NewHybrid(
		milvus.NewRRFReranker(),
		&einoSearchMode.SubRequest{
			VectorField: s.cfg.MilvusVectorField,
			MetricType:  einoMilvus.MetricType(strings.ToUpper(s.cfg.MilvusMetricType)),
			TopK:        topK * 2,
			VectorType:  einoMilvus.DenseVector,
		},
		&einoSearchMode.SubRequest{
			VectorField: s.cfg.MilvusSparseVectorField,
			MetricType:  einoMilvus.BM25,
			TopK:        topK * 2,
			VectorType:  einoMilvus.SparseVector,
		},
	))
	if err != nil {
		return nil, err
	}
	docs, err := retriever.Retrieve(ctx, query, einoMilvus.WithFilter(s.filterExpr(hrID, input)))
	if err != nil {
		return nil, fmt.Errorf("search milvus: %w", err)
	}
	if len(docs) == 0 {
		return []ResumeSemanticSearchItem{}, nil
	}

	return documentsToSemanticItems(docs), nil
}

func (s *resumeRAGSearcher) RecommendByJD(ctx context.Context, hrID uint64, input ResumeRecommendationInput) ([]ResumeSemanticSearchItem, error) {
	queries := recommendationQueries(input)
	if len(queries) == 0 {
		return nil, fmt.Errorf("recommendation query is required")
	}
	milvusClient, err := s.newMilvusClient(ctx)
	if err != nil {
		return nil, err
	}
	defer milvusClient.Close(ctx)

	candidateLimit := normalizeRecommendationLimit(input.Limit)
	evidenceLimit := normalizeEvidenceLimit(input.EvidenceLimit)
	perQueryLimit := candidateLimit * evidenceLimit * 4
	if perQueryLimit < 30 {
		perQueryLimit = 30
	}
	if perQueryLimit > 80 {
		perQueryLimit = 80
	}
	filter := s.recommendationFilterExpr(hrID, input)
	denseRetriever, err := s.newMilvusRetriever(ctx, milvusClient, perQueryLimit, einoSearchMode.NewApproximate(einoMilvus.MetricType(strings.ToUpper(s.cfg.MilvusMetricType))))
	if err != nil {
		return nil, err
	}
	sparseRetriever, err := s.newMilvusRetriever(ctx, milvusClient, perQueryLimit, einoSearchMode.NewSparse(einoMilvus.BM25))
	if err != nil {
		return nil, err
	}
	var (
		mu       sync.Mutex
		allItems []ResumeSemanticSearchItem
	)
	eg, egCtx := errgroup.WithContext(ctx)
	for _, query := range queries {
		query := query
		eg.Go(func() error {
			docs, err := denseRetriever.Retrieve(egCtx, query, einoMilvus.WithFilter(filter))
			if err != nil {
				return fmt.Errorf("dense recommendation search milvus: %w", err)
			}
			items := documentsToSemanticItems(docs)
			normalizeRecommendationRecallScores(items, true)
			mu.Lock()
			allItems = append(allItems, items...)
			mu.Unlock()
			return nil
		})
		eg.Go(func() error {
			docs, err := sparseRetriever.Retrieve(egCtx, query, einoMilvus.WithFilter(filter))
			if err != nil {
				return fmt.Errorf("keyword recommendation search milvus: %w", err)
			}
			items := documentsToSemanticItems(docs)
			normalizeRecommendationRecallScores(items, false)
			mu.Lock()
			allItems = append(allItems, items...)
			mu.Unlock()
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return mergeRecommendationItems(allItems), nil
}

func resultSetsToSemanticItems(results []milvus.ResultSet) ([]ResumeSemanticSearchItem, error) {
	if len(results) == 0 {
		return nil, nil
	}
	items := make([]ResumeSemanticSearchItem, 0)
	for _, result := range results {
		part, err := resultSetToSemanticItems(result)
		if err != nil {
			return nil, err
		}
		items = append(items, part...)
	}
	return items, nil
}

func recommendationEvidenceKey(item ResumeSemanticSearchItem) string {
	if item.ChunkID > 0 {
		return fmt.Sprintf("chunk:%d", item.ChunkID)
	}
	return fmt.Sprintf("%d:%d:%s:%d:%d", item.CandidateID, item.ResumeID, item.SectionType, item.ExperienceIndex, item.ChunkIndex)
}

func normalizeRecommendationRecallScores(items []ResumeSemanticSearchItem, semantic bool) {
	if len(items) == 0 {
		return
	}
	var maxScore float32
	for _, item := range items {
		if item.Score > maxScore {
			maxScore = item.Score
		}
	}
	if maxScore <= 0 {
		maxScore = 1
	}
	for i := range items {
		score := clampFloat32(items[i].Score/maxScore, 0, 1)
		if semantic {
			items[i].SemanticScore = score
		} else {
			items[i].KeywordScore = score
		}
		items[i].Score = recommendationEvidenceScore(items[i].SemanticScore, items[i].KeywordScore)
	}
}

func mergeRecommendationItems(items []ResumeSemanticSearchItem) []ResumeSemanticSearchItem {
	merged := make(map[string]ResumeSemanticSearchItem)
	for _, item := range items {
		key := recommendationEvidenceKey(item)
		old, ok := merged[key]
		if !ok {
			item.Score = recommendationEvidenceScore(item.SemanticScore, item.KeywordScore)
			merged[key] = item
			continue
		}
		if item.SemanticScore > old.SemanticScore {
			old.SemanticScore = item.SemanticScore
		}
		if item.KeywordScore > old.KeywordScore {
			old.KeywordScore = item.KeywordScore
		}
		old.Score = recommendationEvidenceScore(old.SemanticScore, old.KeywordScore)
		merged[key] = old
	}
	out := make([]ResumeSemanticSearchItem, 0, len(merged))
	for _, item := range merged {
		out = append(out, item)
	}
	return out
}

func recommendationEvidenceScore(semanticScore, keywordScore float32) float32 {
	return semanticScore*recommendationSemanticWeight + keywordScore*recommendationKeywordWeight
}

func recommendationQueries(input ResumeRecommendationInput) []string {
	seen := map[string]struct{}{}
	queries := make([]string, 0, len(input.Queries)+1)
	add := func(value string) {
		value = normalizeResumeText(value)
		if value == "" {
			return
		}
		if len([]rune(value)) > 300 {
			value = string([]rune(value)[:300])
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		queries = append(queries, value)
	}
	add(input.JobDescription)
	for _, query := range input.Queries {
		add(query)
	}
	if len(queries) > 5 {
		queries = queries[:5]
	}
	return queries
}

func (s *resumeRAGSearcher) recommendationFilterExpr(hrID uint64, input ResumeRecommendationInput) string {
	filters := []string{fmt.Sprintf("hr_id == %d", hrID)}
	if input.JobID > 0 {
		filters = append(filters, fmt.Sprintf("job_id == %d", input.JobID))
	}
	return strings.Join(filters, " && ")
}

func aggregateRecommendationCandidates(items []ResumeSemanticSearchItem, limit, evidenceLimit int) []ResumeRecommendationCandidate {
	byCandidate := map[uint64]*ResumeRecommendationCandidate{}
	for _, item := range items {
		if item.CandidateID == 0 {
			continue
		}
		candidate := byCandidate[item.CandidateID]
		if candidate == nil {
			candidate = &ResumeRecommendationCandidate{
				CandidateID:   item.CandidateID,
				CandidateName: item.CandidateName,
				ResumeID:      item.ResumeID,
				ResumeName:    item.ResumeName,
				JobID:         item.JobID,
				JobTitle:      item.JobTitle,
			}
			byCandidate[item.CandidateID] = candidate
		}
		candidate.Evidence = append(candidate.Evidence, item)
	}
	candidates := make([]ResumeRecommendationCandidate, 0, len(byCandidate))
	for _, candidate := range byCandidate {
		sort.SliceStable(candidate.Evidence, func(i, j int) bool {
			return candidate.Evidence[i].Score > candidate.Evidence[j].Score
		})
		candidate.SemanticScore = aggregateEvidenceSignal(candidate.Evidence, evidenceLimit, func(item ResumeSemanticSearchItem) float32 {
			return item.SemanticScore
		})
		candidate.KeywordScore = aggregateEvidenceSignal(candidate.Evidence, evidenceLimit, func(item ResumeSemanticSearchItem) float32 {
			return item.KeywordScore
		})
		candidate.Score = recommendationEvidenceScore(candidate.SemanticScore, candidate.KeywordScore)
		if len(candidate.Evidence) > evidenceLimit {
			candidate.Evidence = candidate.Evidence[:evidenceLimit]
		}
		candidates = append(candidates, *candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func aggregateEvidenceSignal(evidence []ResumeSemanticSearchItem, evidenceLimit int, scoreOf func(ResumeSemanticSearchItem) float32) float32 {
	if len(evidence) == 0 {
		return 0
	}
	if evidenceLimit <= 0 {
		evidenceLimit = 3
	}
	topN := minInt(len(evidence), evidenceLimit)
	score := scoreOf(evidence[0]) * recommendationTopEvidenceWeight
	if topN > 1 {
		var rest float32
		for i := 1; i < topN; i++ {
			rest += scoreOf(evidence[i])
		}
		score += (rest / float32(topN-1)) * (1 - recommendationTopEvidenceWeight)
	}
	return score
}

func normalizeRecommendationLimit(limit int) int {
	if limit <= 0 {
		return 5
	}
	if limit > 10 {
		return 10
	}
	return limit
}

func normalizeEvidenceLimit(limit int) int {
	if limit <= 0 {
		return 3
	}
	if limit > 5 {
		return 5
	}
	return limit
}

func resultSetToSemanticItems(result milvus.ResultSet) ([]ResumeSemanticSearchItem, error) {
	if result.Err != nil {
		return nil, result.Err
	}
	var rows []*resumeChunkRow
	if err := result.Unmarshal(&rows); err != nil {
		return nil, fmt.Errorf("unmarshal milvus result: %w", err)
	}
	items := make([]ResumeSemanticSearchItem, 0, len(rows))
	for i, row := range rows {
		score := float32(0)
		if i < len(result.Scores) {
			score = result.Scores[i]
		}
		items = append(items, ResumeSemanticSearchItem{
			Score:   score,
			ChunkID: uint64(row.ChunkID),
			JobID:   uint64(row.JobID),
		})
	}
	return items, nil
}

func resumeDocumentConverter(ctx context.Context, result milvus.ResultSet) ([]*schema.Document, error) {
	_ = ctx
	items, err := resultSetToSemanticItems(result)
	if err != nil {
		return nil, err
	}
	docs := make([]*schema.Document, 0, len(items))
	for _, item := range items {
		doc := (&schema.Document{
			ID:      recommendationEvidenceKey(item),
			Content: item.Content,
			MetaData: map[string]any{
				"resume_item": item,
			},
		}).WithScore(float64(item.Score))
		docs = append(docs, doc)
	}
	return docs, nil
}

func documentsToSemanticItems(docs []*schema.Document) []ResumeSemanticSearchItem {
	items := make([]ResumeSemanticSearchItem, 0, len(docs))
	for _, doc := range docs {
		if item, ok := doc.MetaData["resume_item"].(ResumeSemanticSearchItem); ok {
			item.Score = float32(doc.Score())
			items = append(items, item)
		}
	}
	return items
}

func (s *resumeRAGSearcher) embedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += embeddingBatchSize {
		end := start + embeddingBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		embeddings, err := s.embedTexts(ctx, texts[start:end], "document")
		if err != nil {
			return nil, err
		}
		out = append(out, embeddings...)
	}
	return out, nil
}

func (s *resumeRAGSearcher) embedTexts(ctx context.Context, texts []string, textType string) ([][]float32, error) {
	_ = textType
	vectors, err := s.embedder.EmbedStrings(ctx, texts, einoembedding.WithModel(s.cfg.EmbeddingModel))
	if err != nil {
		return nil, err
	}
	return float64VectorsToFloat32(vectors)
}

func newDashScopeOpenAIEmbedder(ctx context.Context, cfg config.RAGConfig) (einoembedding.Embedder, error) {
	if cfg.EmbeddingAPIKey == "" {
		return nil, fmt.Errorf("rag embedding api key is required")
	}
	if cfg.EmbeddingEndpoint == "" {
		return nil, fmt.Errorf("rag embedding endpoint is required")
	}
	if cfg.EmbeddingModel == "" {
		return nil, fmt.Errorf("rag embedding model is required")
	}
	embeddingConfig := &einoOpenAI.EmbeddingConfig{
		APIKey:  cfg.EmbeddingAPIKey,
		BaseURL: normalizeEmbeddingBaseURL(cfg.EmbeddingEndpoint),
		Model:   cfg.EmbeddingModel,
	}
	if cfg.EmbeddingTimeoutSeconds > 0 {
		embeddingConfig.Timeout = time.Duration(cfg.EmbeddingTimeoutSeconds) * time.Second
	}
	if cfg.EmbeddingDimension > 0 {
		dim := cfg.EmbeddingDimension
		embeddingConfig.Dimensions = &dim
	}
	return einoOpenAI.NewEmbedder(ctx, embeddingConfig)
}

func normalizeEmbeddingBaseURL(endpoint string) string {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" || strings.Contains(endpoint, "/api/v1/services/embeddings/") {
		return "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	return endpoint
}

func (s *resumeRAGSearcher) filterExpr(hrID uint64, input ResumeSemanticSearchInput) string {
	filters := []string{fmt.Sprintf("hr_id == %d", hrID)}
	if input.JobID > 0 {
		filters = append(filters, fmt.Sprintf("job_id == %d", input.JobID))
	}
	return strings.Join(filters, " && ")
}

func normalizeRAGLimit(inputLimit int, defaultLimit int) int {
	if inputLimit <= 0 {
		inputLimit = defaultLimit
	}
	if inputLimit <= 0 {
		return 8
	}
	if inputLimit > 20 {
		return 20
	}
	return inputLimit
}

func normalizeResumeText(value string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range value {
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		if !unicode.IsPrint(r) {
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimSpace(b.String())
}

func normalizeResumeLines(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = normalizeResumeText(line)
		if line == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
				blank = true
			}
			continue
		}
		out = append(out, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func buildExperienceChunksFromProfile(profile domain.CandidateProfile) []resumeExperienceChunk {
	chunks := make([]resumeExperienceChunk, 0, resumeMaxChunks)
	sections := []resumeSection{
		{
			Type:  "work",
			Title: "工作经历",
			Lines: meaningfulResumeLines(profile.WorkExperience),
			Index: 1,
		},
		{
			Type:  "project",
			Title: "项目经历",
			Lines: meaningfulResumeLines(profile.ProjectExperience),
			Index: 1,
		},
	}
	if len(sections[0].Lines) == 0 && len(sections[1].Lines) == 0 {
		sections = []resumeSection{{
			Type:  "experience",
			Title: "经历证据",
			Lines: meaningfulResumeLines(profile.Experience),
			Index: 1,
		}}
	}
	for _, section := range sections {
		if len(section.Lines) == 0 {
			continue
		}
		sectionChunks := chunkExperienceSection(section)
		for _, chunk := range sectionChunks {
			chunks = append(chunks, chunk)
			if len(chunks) >= resumeMaxChunks {
				return chunks
			}
		}
	}
	return chunks
}

type resumeSection struct {
	Type  string
	Title string
	Lines []string
	Index int
}

func meaningfulResumeLines(text string) []string {
	raw := strings.Split(normalizeResumeLines(text), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = normalizeResumeText(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func chunkExperienceSection(section resumeSection) []resumeExperienceChunk {
	body := strings.Join(section.Lines, "\n")
	if body == "" {
		return nil
	}
	title := section.Title
	parts := splitResumeChunks(body)
	chunks := make([]resumeExperienceChunk, 0, len(parts))
	for i, part := range parts {
		content := fmt.Sprintf("%s\n\n%s", title, part)
		chunks = append(chunks, resumeExperienceChunk{
			SectionType:     section.Type,
			SectionTitle:    title,
			ExperienceIndex: section.Index,
			ChunkIndex:      i,
			Content:         content,
			SearchText:      buildSearchText(section.Type, title, content),
		})
	}
	return chunks
}

func buildSearchText(sectionType, title, content string) string {
	seen := map[string]struct{}{}
	parts := make([]string, 0, 64)
	add := func(value string) {
		value = normalizeResumeText(strings.Trim(value, " ,，.。;；:：|/\\()（）[]【】{}<>《》\"'`"))
		if value == "" {
			return
		}
		if len([]rune(value)) > 40 {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		parts = append(parts, value)
	}

	add(sectionType)
	add(title)
	normalized := normalizeResumeText(content)
	lower := strings.ToLower(normalized)
	for _, keyword := range resumeKeywordVocabulary {
		if strings.Contains(lower, strings.ToLower(keyword)) {
			add(keyword)
		}
	}
	tokens := strings.FieldsFunc(normalized, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(".,，。;；:：|/\\()（）[]【】{}<>《》\"'`、+-=*#\n\t", r)
	})
	for _, token := range tokens {
		if !hasASCIIAlphaNum(token) {
			continue
		}
		add(token)
		if len(parts) >= 160 {
			break
		}
	}
	return normalizeResumeText(strings.Join(parts, " "))
}

var resumeKeywordVocabulary = []string{
	"Go", "Golang", "Java", "Python", "TypeScript", "JavaScript", "Vue", "React", "Node.js",
	"gRPC", "HTTP", "REST", "微服务", "分布式", "高并发", "高可用", "限流", "熔断", "负载均衡",
	"MySQL", "Redis", "MongoDB", "PostgreSQL", "Elasticsearch", "Kafka", "RabbitMQ", "RocketMQ",
	"Docker", "Kubernetes", "K8s", "Linux", "Nginx", "CI/CD", "DevOps",
	"RAG", "向量检索", "Milvus", "Embedding", "大模型", "Agent", "推荐系统", "搜索",
	"订单", "支付", "交易", "电商", "风控", "库存", "营销", "报表", "数据分析", "数据仓库",
	"项目经历", "实习经历", "工作经历", "后端", "前端", "全栈", "数据库", "缓存", "消息队列",
	"性能优化", "索引优化", "接口设计", "权限", "鉴权", "监控", "日志", "测试", "部署",
}

func hasASCIIAlphaNum(value string) bool {
	for _, r := range value {
		if r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return true
		}
	}
	return false
}

func splitResumeChunks(text string) []string {
	text = normalizeResumeLines(text)
	if text == "" {
		return nil
	}
	chunks := recursiveSplitResumeChunks(text, []string{"\n\n", "\n", "。", "；", "，", " "})
	out := make([]string, 0, minInt(len(chunks), resumeMaxChunks))
	for _, chunk := range chunks {
		chunk = normalizeResumeText(chunk)
		if chunk == "" {
			continue
		}
		out = append(out, chunk)
		if len(out) >= resumeMaxChunks {
			break
		}
	}
	return out
}

func recursiveSplitResumeChunks(text string, separators []string) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	if len(runes) <= resumeChunkSize {
		return []string{strings.TrimSpace(text)}
	}
	if len(separators) == 0 {
		return splitLongTextByWindow(text)
	}

	separator := separators[0]
	rawParts := strings.Split(text, separator)
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) <= 1 {
		return recursiveSplitResumeChunks(text, separators[1:])
	}

	var chunks []string
	var current strings.Builder
	flush := func() {
		value := strings.TrimSpace(current.String())
		if value != "" {
			chunks = append(chunks, value)
		}
		current.Reset()
	}
	for _, part := range parts {
		partLen := len([]rune(part))
		if partLen > resumeChunkSize {
			flush()
			chunks = append(chunks, recursiveSplitResumeChunks(part, separators[1:])...)
			continue
		}
		next := part
		if current.Len() > 0 && separator != " " {
			next = separator + part
		} else if current.Len() > 0 {
			next = " " + part
		}
		if len([]rune(current.String()+next)) > resumeChunkSize {
			flush()
			current.WriteString(part)
			continue
		}
		current.WriteString(next)
	}
	flush()
	return chunks
}

func splitLongTextByWindow(text string) []string {
	runes := []rune(normalizeResumeText(text))
	chunks := make([]string, 0, minInt(resumeMaxChunks, len(runes)/resumeChunkSize+1))
	for start := 0; start < len(runes) && len(chunks) < resumeMaxChunks; {
		end := minInt(start+resumeChunkSize, len(runes))
		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end == len(runes) {
			break
		}
		start = end
	}
	return chunks
}

func vectorDim(vectors [][]float32) (int, error) {
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return 0, fmt.Errorf("empty embedding vectors")
	}
	dim := len(vectors[0])
	for i, vector := range vectors {
		if len(vector) != dim {
			return 0, fmt.Errorf("embedding vector %d dimension mismatch: got %d want %d", i, len(vector), dim)
		}
	}
	return dim, nil
}

func float64VectorsToFloat32(vectors [][]float64) ([][]float32, error) {
	if len(vectors) == 0 {
		return nil, fmt.Errorf("empty embedding vectors")
	}
	out := make([][]float32, 0, len(vectors))
	for i, vector := range vectors {
		if len(vector) == 0 {
			return nil, fmt.Errorf("embedding vector %d is empty", i)
		}
		converted := make([]float32, len(vector))
		for j, value := range vector {
			converted[j] = float32(value)
		}
		out = append(out, converted)
	}
	return out, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func joinUint64s(values []uint64) string {
	var b strings.Builder
	for i, value := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d", value)
	}
	return b.String()
}
