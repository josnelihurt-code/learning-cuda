package remote

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockDevicePower struct {
	mock.Mock
}

func (m *mockDevicePower) PowerOn() error {
	return m.Called().Error(0)
}

type mockAcceleratorHealth struct {
	mock.Mock
}

func (m *mockAcceleratorHealth) IsAvailable() bool {
	return m.Called().Bool(0)
}

func TestStartJetsonNanoUseCase_Execute(t *testing.T) {
	errPower := errors.New("mqtt down")

	tests := []struct {
		name         string
		power        *mockDevicePower
		nilPower     bool
		setup        func(*mockDevicePower)
		assertResult func(*testing.T, StartJetsonNanoUseCaseOutput, error)
	}{
		{
			name: "Success_PowerOn",
			setup: func(m *mockDevicePower) {
				m.On("PowerOn").Return(nil).Once()
			},
			assertResult: func(t *testing.T, out StartJetsonNanoUseCaseOutput, err error) {
				assert.NoError(t, err)
				assert.True(t, out.Success)
				assert.Equal(t, "sent", out.Step)
			},
		},
		{
			name: "Success_PowerOnFailedReturnsOutput",
			setup: func(m *mockDevicePower) {
				m.On("PowerOn").Return(errPower).Once()
			},
			assertResult: func(t *testing.T, out StartJetsonNanoUseCaseOutput, err error) {
				assert.NoError(t, err)
				assert.False(t, out.Success)
				assert.Equal(t, "error", out.Step)
				assert.Contains(t, out.Message, "Failed to send power command")
			},
		},
		{
			name:     "Error_NilPower",
			nilPower: true,
			assertResult: func(t *testing.T, out StartJetsonNanoUseCaseOutput, err error) {
				assert.Error(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			var power devicePower
			var mockPower *mockDevicePower
			if !tt.nilPower {
				mockPower = new(mockDevicePower)
				if tt.setup != nil {
					tt.setup(mockPower)
				}
				power = mockPower
			}
			sut := NewStartJetsonNanoUseCase(power)

			// Act
			out, err := sut.Execute(context.Background(), StartJetsonNanoUseCaseInput{})

			// Assert
			tt.assertResult(t, out, err)
			if mockPower != nil {
				mockPower.AssertExpectations(t)
			}
		})
	}
}

func TestCheckAcceleratorHealthUseCase_Execute(t *testing.T) {
	t.Run("Success_Healthy", func(t *testing.T) {
		// Arrange
		health := new(mockAcceleratorHealth)
		health.On("IsAvailable").Return(true).Once()
		sut := NewCheckAcceleratorHealthUseCase(health)

		// Act
		out, err := sut.Execute(context.Background(), CheckAcceleratorHealthUseCaseInput{})

		// Assert
		require.NoError(t, err)
		assert.True(t, out.Healthy)
		health.AssertExpectations(t)
	})

	t.Run("Success_Unhealthy", func(t *testing.T) {
		// Arrange
		health := new(mockAcceleratorHealth)
		health.On("IsAvailable").Return(false).Once()
		sut := NewCheckAcceleratorHealthUseCase(health)

		// Act
		out, err := sut.Execute(context.Background(), CheckAcceleratorHealthUseCaseInput{})

		// Assert
		require.NoError(t, err)
		assert.False(t, out.Healthy)
		assert.Equal(t, "no accelerator registered", out.Message)
		health.AssertExpectations(t)
	})
}
