package product

import "context"

type repository interface {
	FindAll(ctx context.Context) ([]Product, error)
}

type Service struct {
	repo repository
}

func NewService(repo repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) FindAll(ctx context.Context) ([]ProductResponse, error) {

	products, err := s.repo.FindAll(ctx)
	if err != nil {
		return nil, err
	}

	responses := make([]ProductResponse, 0, len(products))
	for _, p := range products {
		responses = append(responses, toResponse(p))
	}

	return responses, nil
}
