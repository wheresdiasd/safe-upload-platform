# Rationale
Uploading documents requires a protected system, where users can upload files quickly without compromising the system security. This project delivers a file upload system that is fast, secure and can be used across any areas of the platform.

# Goals
* The system should be scalable and initially attend to:
* * Scale: How many files? (10M+ users, 50TB+ storage).
* * File Size: Mostly PDFs and Images (average 2MB, max 20MB).
* Increase the upload speed and create seamless CX.
* Mitigate security breaches
* Keep file metadata database

# Proposed architecture
![Safe File Upload System - Page 1 High level design](https://github.com/user-attachments/assets/a8a21276-10a9-4d57-bc43-18ea78d265db)

![Safe File Upload System - Page 1 File creation](https://github.com/user-attachments/assets/9aceaa86-818b-4aa0-b9a6-7cb783634b41)
![Safe File Upload System - Page 1 File update](https://github.com/user-attachments/assets/661165c6-8f43-43b2-8414-170e8f0e78f7)
![Safe File Upload System - Page 2 Upload file](https://github.com/user-attachments/assets/a127c693-20e4-4eae-a28a-39e265ae8cbb)
