// Package storage хранит файлы (шаблоны и документы) в S3-совместимом хранилище.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ErrNotFound — объекта с таким ключом нет.
var ErrNotFound = errors.New("object not found")

// Config — параметры подключения к S3.
type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Region    string
}

// Store — клиент файлового хранилища.
type Store struct {
	client *minio.Client
	region string
}

// New создаёт клиент. Соединение при этом не открывается.
func New(cfg Config) (*Store, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}

	return &Store{
		client: client,
		region: cfg.Region,
	}, nil
}

// EnsureBucket создаёт бакет, если его ещё нет.
func (s *Store) EnsureBucket(ctx context.Context, bucket string) error {
	exists, err := s.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check bucket %q: %w", bucket, err)
	}
	if exists {
		return nil
	}

	if err := s.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: s.region}); err != nil {
		return fmt.Errorf("create bucket %q: %w", bucket, err)
	}

	return nil
}

// Put сохраняет объект. size — точный размер в байтах.
func (s *Store) Put(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("put %s/%s: %w", bucket, key, err)
	}

	return nil
}

// Get открывает объект на чтение. Вызывающий обязан закрыть результат.
func (s *Store) Get(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	reader, err := s.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s/%s: %w", bucket, key, err)
	}

	if _, err := reader.Stat(); err != nil {
		_ = reader.Close()
		if minio.ToErrorResponse(err).Code == minio.NoSuchKey {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("stat %s/%s: %w", bucket, key, err)
	}

	return reader, nil
}

// Delete удаляет объект. Удаление несуществующего объекта не считается ошибкой.
func (s *Store) Delete(ctx context.Context, bucket, key string) error {
	if err := s.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete %s/%s: %w", bucket, key, err)
	}

	return nil
}
