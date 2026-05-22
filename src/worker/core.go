package worker

import (
	"encoding/json"
	"fmt"
	"log"
	"membox-serv/src/types"
	"sync"
	"time"

	db "membox-serv/src/db_layer"

	"github.com/huandu/go-sqlbuilder"
)

type Worker struct {
	is_init        bool
	Job_name       string
	Serv_name      string
	Job_func       Worker_job_func
	instance_state []Instance_state
	kickMutex      sync.Mutex
}

type Instance_state struct {
	Is_running bool
	Job_uid    string
}

type Worker_job_func func(
	input types.Js_object,
	user_uid string,
) (output types.Js_object, err error)

type job struct {
	UID      string
	Input    types.Js_object
	User_uid string
}

func (w *Worker) Init(instance_count int) (err error) {
	log.Printf("[worker:%s] initializing with %d instances", w.Job_name, instance_count)

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("jobs").Set(
		ub.Assign("locked_at", nil),
		ub.Assign("locked_by", ""),
	).Where(
		ub.Equal("name", w.Job_name),
		ub.Equal("locked_by", w.Serv_name),
	)

	err = db.Exec(ub)
	if err != nil {
		log.Printf("[worker:%s] failed to clear stale db locks: %v", w.Job_name, err)
		return
	}

	for i := 0; i < instance_count; i++ {
		w.instance_state = append(w.instance_state, Instance_state{
			Is_running: false,
			Job_uid:    "",
		})
	}

	w.is_init = true
	log.Printf("[worker:%s] initialized successfully", w.Job_name)
	w.Kick()

	return nil

}

func (w *Worker) Get_jobs(count int) (jobs []job, err error) {
	bldr := sqlbuilder.BuildNamed(`
        UPDATE jobs 
        SET locked_at = NOW(), locked_by = ${serv_name}, status = 'running'
        WHERE uid IN (
            SELECT uid FROM jobs 
            WHERE name = ${job_name} AND status = 'queued' AND locked_by = ''
            ORDER BY created_at ASC
            LIMIT ${count}
            FOR UPDATE SKIP LOCKED
        )
        RETURNING uid, input, user_uid
    `, map[string]interface{}{
		"serv_name": w.Serv_name,
		"job_name":  w.Job_name,
		"count":     count,
	})

	res, err := db.Query_all(bldr)
	if err != nil {
		log.Printf("[worker:%s] failed to fetch jobs: %v", w.Job_name, err)
		return
	}

	for _, row := range res {
		job := job{
			UID: string(row[0]),
		}

		err = json.Unmarshal(row[1], &job.Input)
		if err != nil {
			log.Printf("[worker:%s] failed to unmarshal job input: %v", w.Job_name, err)
			return
		}
		job.User_uid = string(row[2])
		jobs = append(jobs, job)
	}

	fmt.Println("jobs:", jobs)

	return

}

func (w *Worker) Kick() (err error) {
	if !w.is_init {
		return fmt.Errorf("worker is not init")
	}

	if !w.kickMutex.TryLock() {
		// Another kick is already in progress, skip this one
		return nil
	}
	defer w.kickMutex.Unlock()

	log.Printf("[worker:%s] kicking", w.Job_name)

	empty_instance_indexes := []int{}
	for i, instance := range w.instance_state {
		if !instance.Is_running {
			empty_instance_indexes = append(empty_instance_indexes, i)
		}
	}

	if len(empty_instance_indexes) == 0 {
		log.Printf("[worker:%s] no empty instances", w.Job_name)
		return nil
	}

	jobs, err := w.Get_jobs(len(empty_instance_indexes))
	if err != nil {
		return err
	}

	if len(jobs) == 0 {
		return nil
	}

	loop_cnt := min(len(empty_instance_indexes), len(jobs))
	log.Printf("[worker:%s] picked up %d job(s)", w.Job_name, loop_cnt)

	for i := 0; i < loop_cnt; i++ {
		instance := &w.instance_state[empty_instance_indexes[i]]
		currjob := jobs[i]

		instance.Is_running = true
		instance.Job_uid = currjob.UID

		go (func(inst *Instance_state, j job) {
			inst.run_instance(
				j,
				w.Job_func,
				w.Job_name,
				j.User_uid,
				func() {
					w.Kick()
				},
			)
		})(instance, currjob)
	}

	return nil

}

func (inst *Instance_state) run_instance(job job, job_func Worker_job_func, job_name string, user_uid string, onDone func()) {
	output := types.Js_object{}
	err := error(nil)
	start := time.Now()

	log.Printf("[worker:%s] starting job %s", job_name, job.UID)

	defer (func() {
		r := recover()
		if r != nil {
			err = fmt.Errorf("panic: %v", r)
			log.Printf("[worker:%s] job %s panicked: %v", job_name, job.UID, r)
		}

		status := "succeeded"

		if err != nil {
			status = "failed"
			if output == nil {
				output = types.Js_object{}
			}
			output["err"] = err.Error()
			log.Printf("[worker:%s] job %s failed: %v (took %v)", job_name, job.UID, err, time.Since(start))
		} else {
			log.Printf("[worker:%s] job %s succeeded (took %v)", job_name, job.UID, time.Since(start))
		}

		inst.Is_running = false
		inst.Job_uid = ""

		ub := sqlbuilder.NewUpdateBuilder()
		ub.Update("jobs").Set(
			ub.Assign("status", status),
			ub.Assign("output", output.Json()),
			ub.Assign("locked_at", nil),
			ub.Assign("locked_by", ""),
			ub.Assign("updated_at", time.Now()),
		).Where(ub.Equal("uid", job.UID))
		err = db.Exec(ub)
		if err != nil {
			log.Printf("[worker:%s] failed to update job %s status: %v", job_name, job.UID, err)
			return
		}

		onDone()

	})()

	output, err = job_func(job.Input, user_uid)
	if err != nil {
		return
	}

}
