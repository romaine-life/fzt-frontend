import { AppConfigurationClient } from '@azure/app-configuration';
import { DefaultAzureCredential } from '@azure/identity';

export async function fetchConfig() {
  const appConfigEndpoint = process.env.AZURE_APP_CONFIG_ENDPOINT;
  if (!appConfigEndpoint) throw new Error('AZURE_APP_CONFIG_ENDPOINT unset');

  const credential = new DefaultAzureCredential();
  const appConfig = new AppConfigurationClient(appConfigEndpoint, credential);

  const cosmosEndpoint = await appConfig.getConfigurationSetting({ key: 'cosmos_db_endpoint' });

  return {
    cosmosDbEndpoint: cosmosEndpoint.value,
  };
}
