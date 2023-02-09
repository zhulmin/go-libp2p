package async

import (
	"sync"
	"testing"
)

func TestSignal(t *testing.T) {
	var v CondVar

	const numTasks = 3

	ok := make([]bool, numTasks)
	var start, stop sync.WaitGroup

	for i := 0; i < numTasks; i++ {
		i := i
		start.Add(1)
		stop.Add(1)
		go func() {
			ch := v.Wait()
			start.Done()
			<-ch
			ok[i] = true
			stop.Done()
		}()
	}

	start.Wait()
	v.Signal()
	stop.Wait()

	for i, b := range ok {
		if !b {
			t.Errorf("Task %d did not report success", i+1)
		}
	}
}
