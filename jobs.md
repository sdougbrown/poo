Background jobs:
- A tool that says it may run in the background takes a "background": true argument. The call returns at once with a job handle, and the job's result arrives later as a message.
- Use the jobs tool to see the jobs that are running, and cancel_job to stop one. There is no wait_job: ending your turn is how you wait.
- A job keeps running when the turn ends and when the run is cancelled, so a long command belongs in one.
