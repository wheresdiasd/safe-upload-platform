# Rationale
As MR Fingers needs to upload documents on a system, where users from WEB/APPS/API's can upload files quickly without compromising the system security and file integrity. This project delivers a file upload system that is fast, secure and can be used across any areas of the platform and with a set of policies that protects the final user of having files exposed, even within the organisation. 

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

# User/Client
The client component is the door to the upload platform, it allows final users to upload files via web interface or app, it can also be an API based as is possible to request pre-signed urls via API if you have a valid auth token.

# API Gateway
The API gateway will be the keeper between the application and the client, with exception for the presigned urls upload ( which is handled by AWS S3 ). It should have rate-limit rules and check authorization via Incognito ( which is not a part of this TDD ). We will configure only 1 endpoint on the gateway ( refer to file creation image above ): 

### /get-pressigned-urls

This endpoint will only be called for auth users who can upload documents on their account. 
** Document must have maximum of 20mb. 
** Should scale for 10M+ users, 50TB+ storage.

# S3 Bucket
This is our go to as 

