#!/bin/bash

# Build the Docker image
echo "Building Docker image..."
docker build -t eink-timetable .

# Run the container
echo "Starting container..."
docker run -d \
  --name eink-timetable \
  -p 8080:8080 \
  -v "$(pwd)/display.html:/app/display.html" \
  -v "$(pwd)/trams.py:/app/trams.py" \
  --restart unless-stopped \
  eink-timetable

echo "Container started! Access at http://localhost:8080"
echo "To stop: docker stop eink-timetable"
echo "To view logs: docker logs eink-timetable"