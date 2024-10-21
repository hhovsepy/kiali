# Use a base image with Node.js and Git installed
FROM cypress/base:latest

# Install git
RUN apt-get update && \
    apt-get install -y git

# Set the working directory in the container
WORKDIR /app

# Copy package.json and package-lock.json to the working directory
COPY package*.json ./

# Copy the rest of the application code
COPY . .

# Your additional configuration (if any)

