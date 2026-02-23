# Rationale
Uploading documents requires a protected system, where users can upload files quickly without compromising the system security. This project delivers a file upload system that is fast, secure and can be used across any areas of the platform.

# Goals
* The system should be scalable and initially attend to:
* * Scale: How many files? (10M+ users, 50TB+ storage).
* * File Size: Mostly PDFs and Images (average 2MB, max 20MB).
* Increase the upload speed on the client side significantly
* Eliminate security breaches
* Keep a record of all file submissions
* Eliminate friction with the client side, ensuring a smooth customer experience
* Establish a white-label upload platform that can be a part of CloudFormation, and a re-usable component.

# Proposed architecture
![Safe File Upload System - Page 1 High level design](https://github.com/user-attachments/assets/a8a21276-10a9-4d57-bc43-18ea78d265db)
